//ToDo:

package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

type Cfg struct {
	EnableTLS          bool   `yaml:"EnableTLS"`
	EnableNoTLS        bool   `yaml:"EnableNoTLS"`
	EnableFDP          bool   `yaml:"EnableFDP"`
	EnablePHL          bool   `yaml:"EnablePHL"`
	MaxUploadSize      int64  `yaml:"MaxUploadSize"`
	DaysOld            int    `yaml:"DaysOld"`
	DelVidAftUpl       bool   `yaml:"DelVidAftUpl"`
	CertPathCrt        string `yaml:"CertPathCrt"`
	CertPathKey        string `yaml:"CertPathKey"`
	ServerPort         string `yaml:"ServerPort"`
	ServerPortTLS      string `yaml:"ServerPortTLS"`
	BindtoAdress       string `yaml:"BindtoAdress"`
	MaxVideosPerHour   int    `yaml:"MaxVideosPerHour"`
	VideoPerPage       int    `yaml:"VideoPerPage"`
	MaxVideoNameLen    int    `yaml:"MaxVideoNameLen"`
	VideoResLow        string `yaml:"VideoResLow"`
	VideoResMed        string `yaml:"VideoResMed"`
	VideoResHigh       string `yaml:"VideoResHigh"`
	BitRateLow         string `yaml:"BitRateLow"`
	BitRateMed         string `yaml:"BitRateMed"`
	BitRateHigh        string `yaml:"BitRateHigh"`
	UploadPath         string `yaml:"UploadPath"`
	ConvertPath        string `yaml:"ConvertPath"`
	CheckOldEvery      string `yaml:"CheckOldEvery"`
	AllowUploadWithPsw bool   `yaml:"AllowUploadWithPsw"`
	Psw                string `yaml:"Psw"`
	NrOfCoreVideoConv  string `yaml:"NrOfCoreVideoConv"`
}

type folderInfo struct {
	Name    string
	ModTime time.Time
}

type folderInfos []folderInfo

var (
	AppConfig       Cfg
	checkOldEvery   = time.Hour //wait time before recheck  file deletion policies
	safeFileName    = regexp.MustCompile("^[a-zA-Z0-9_-]+(\\.[a-zA-Z0-9_]+)*$")
	videosUploaded  int
	quequelen       int = 0
	templatefl          = template.Must(template.ParseFiles("pages/filelist.html"))
	templateq           = template.Must(template.ParseFiles("pages/queque.html"))
	templateupl         = template.Must(template.ParseFiles("pages/uploaded.html"))
	templatevp          = template.Must(template.ParseFiles("pages/vp.html"))
	templatevpnojs      = template.Must(template.ParseFiles("pages/vpnojs.html"))
	templateerr         = template.Must(template.ParseFiles("pages/error.html"))
	templatesndfile     = template.Must(template.ParseFiles("pages/sendfile.html"))
	videoQuality        = make(chan VideoParams)
	channelOpen         = false
)

const (
	configPath   = "./config.yaml"
	staticPath   = "./static"
	faviconPath  = "./static/favicon.ico"
	sendfilePath = "./static/sendfile.html"
)

type VideoParams struct {
	videoPath    string
	ConvertPath  string
	quality      string
	width        string
	height       string
	audio        bool
	processaudio bool
	audioquality string
	creatempd    bool
	videoName    string
	createThunb  bool
}

type PageList struct {
	Files     []folderInfo
	PrevPage  int
	NextPage  int
	TotalPage int
}

type PageQueque struct {
	QuequeSize int
}

type PageUploaded struct {
	FileName      string
	FileNameNoExt string
	QuequeSize    int
}
type PageVP struct {
	VidNm string
}
type PageVPNoJS struct {
	VidNm string
}
type PageErr struct {
	ErrMsg string
}
type PageSndFile struct {
	UseAuth bool
}

func main() {
	ReadConfig()

	if AppConfig.EnableFDP {
		go deleteOLD()
	}
	d, err := time.ParseDuration(AppConfig.CheckOldEvery)
	if err != nil {
		fmt.Println("Error parsing CheckOldEvery from config.yaml. Using default value (1h)", err)
		d = time.Hour
	}
	checkOldEvery = d

	go resetVideoUploadedCounter()
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/video", handleVideo)
	http.HandleFunc("/vp", handleVP)
	http.HandleFunc("/Send", handleSendVideo)
	http.HandleFunc("/", http.HandlerFunc(listfolderhandler))
	http.Handle("/static/", http.StripPrefix("/static", http.FileServer(http.Dir(staticPath))))
	http.Handle("/converted/", http.StripPrefix("/converted", http.FileServer(http.Dir("./converted"))))
	http.HandleFunc("/favicon.ico", http.HandlerFunc(faviconHandler))
	http.HandleFunc("/lst", listfolderhandler)
	http.HandleFunc("/queque", quequesize)
	if AppConfig.EnableTLS {
		go func() {
			err := http.ListenAndServeTLS(AppConfig.BindtoAdress+":"+AppConfig.ServerPortTLS, AppConfig.CertPathCrt, AppConfig.CertPathKey, nil)
			if err != nil {
				fmt.Println(err)
			}
		}()
	}
	if AppConfig.EnableNoTLS {
		err := http.ListenAndServe(AppConfig.BindtoAdress+":"+AppConfig.ServerPort, nil)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, faviconPath)
}

func quequesize(w http.ResponseWriter, r *http.Request) {
	p := &PageQueque{
		QuequeSize: quequelen,
	}
	renderTemplate(w, "queque", p)
}

func listfolderhandler(w http.ResponseWriter, r *http.Request) {
	pageNum, err := strconv.Atoi(r.FormValue("page"))
	if err != nil || pageNum < 1 {
		pageNum = 1
	}

	dirPath := "converted"
	folders, err := listFolders(dirPath, pageNum)
	if err != nil {
		senderror(w, r, err.Error())
		return
	}

	data := &PageList{
		Files: folders,
	}

	if pageNum > 1 {
		data.PrevPage = pageNum - 1
	}

	if len(folders) == AppConfig.VideoPerPage {
		data.NextPage = pageNum + 1
	}

	data.TotalPage = (len(folders) + (AppConfig.VideoPerPage-1)) / AppConfig.VideoPerPage

	renderTemplate(w, "filelist", data)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("video")
	username := r.FormValue("username")
	password := r.FormValue("password")

	if AppConfig.AllowUploadWithPsw && !verifyPassword(username, password) {
		time.Sleep(5000)
		senderror(w, r, "Wrong password")
		return
	}

	var errormsg string
	if err != nil {
		senderror(w, r, err.Error())
		return
	}
	defer file.Close()

	if header.Size > AppConfig.MaxUploadSize {
		errormsg = "The uploaded file is too big: " + header.Filename + ". Max size allowed: " + strconv.FormatInt(AppConfig.MaxUploadSize, 10)
	}
	filename := header.Filename
	if len(filename) > AppConfig.MaxVideoNameLen || !isSafeFileName(filename) {
		errormsg = "Invalid file name: either it contains invalid characters or it's longer than " + strconv.Itoa(AppConfig.MaxVideoNameLen) + " characters"
	}

	//check if the maxium video per h is reached
	if videosUploaded >= AppConfig.MaxVideosPerHour {
		errormsg = "Can't upload more than" + strconv.Itoa(AppConfig.MaxVideosPerHour) + "videos per hour"
	}

	if errormsg != "" {
		senderror(w, r, errormsg)
		return
	}

	extension := path.Ext(filename)
	filenamenoext := strings.TrimSuffix(filename, extension)
	filePath := filepath.Join(AppConfig.UploadPath, filename)
	// Check if the file already exists
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		errormsg = "File already exists: " + filename
		senderror(w, r, errormsg)
		return
	}

	out, err := os.Create(filePath)
	if err != nil {
		senderror(w, r, err.Error())
		return
	}
	defer out.Close()

	videosUploaded++

	_, err = io.Copy(out, file)
	if err != nil {
		senderror(w, r, err.Error())
		return
	}

	go StartconvertVideo(filePath, AppConfig.ConvertPath, filenamenoext)
	p := &PageUploaded{
		FileName:      filename,
		FileNameNoExt: filenamenoext,
		QuequeSize:    quequelen,
	}
	renderTemplate(w, "uploaded", p)
}

func StartconvertVideo(filePath string, ConvertPath string, filenamenoext string) {
	ConvertedLowPath := filepath.Join(ConvertPath+"/"+filenamenoext, "low_"+filenamenoext+".mp4")
	ConvertedLowPathAudio := filepath.Join(ConvertPath+"/"+filenamenoext, "low_"+filenamenoext+"_audio.webm")
	ConvertedMedPath := filepath.Join(ConvertPath+"/"+filenamenoext, "med_"+filenamenoext+".mp4")
	ConvertedHighPath := filepath.Join(ConvertPath+"/"+filenamenoext, "high_"+filenamenoext+".mp4")
	ConvertedAudioPath := filepath.Join(ConvertPath+"/"+filenamenoext, "audio_"+filenamenoext+".mp4")
	Thumbpath := filepath.Join(ConvertPath+"/"+filenamenoext, "output.jpeg")
	MPDPath := filepath.Join(ConvertPath+"/"+filenamenoext, "output.mpd")
	err := os.Mkdir(ConvertPath+"/"+filenamenoext, 0755)
	if err != nil {
		fmt.Println(err)
		return
	}
	quequelen += 7

	if !channelOpen {
		go convertVideo(videoQuality)
		channelOpen = true
	}
	var wg sync.WaitGroup
	var wglowqualityconv sync.WaitGroup
	wglowqualityconv.Add(2) //convert low quality and create thumbnail befor all other conversion, so as soon as possible a low quality video can be played
	wg.Add(4)

	go func() {
		//convert video, with audio for fallback reprodution
		videoQuality <- VideoParams{filePath, ConvertedLowPathAudio, AppConfig.BitRateLow, AppConfig.VideoResLow, "-2", true, false, "64k", false, filenamenoext, false}
		defer wglowqualityconv.Done()
	}()
	go func() {
		//create thumbnail
		videoQuality <- VideoParams{filePath, Thumbpath, AppConfig.BitRateHigh, AppConfig.VideoResHigh, "-2", false, false, "64k", false, filenamenoext, true}
		defer wglowqualityconv.Done()
	}()
	wglowqualityconv.Wait()
	go func() {
		//convert video, no audio for mpd
		videoQuality <- VideoParams{filePath, ConvertedLowPath, AppConfig.BitRateLow, AppConfig.VideoResLow, "-2", false, false, "64k", false, filenamenoext, false}
		defer wg.Done()
	}()

	go func() {
		//convert video, no audio for mpd
		videoQuality <- VideoParams{filePath, ConvertedMedPath, AppConfig.BitRateMed, AppConfig.VideoResMed, "-2", false, false, "64k", false, filenamenoext, false}
		defer wg.Done()
	}()

	go func() {
		//convert video, no audio for mpd
		videoQuality <- VideoParams{filePath, ConvertedHighPath, AppConfig.BitRateHigh, AppConfig.VideoResHigh, "-2", false, false, "64k", false, filenamenoext, false}
		defer wg.Done()
	}()

	go func() {
		//convert audio for mpd
		videoQuality <- VideoParams{filePath, ConvertedAudioPath, AppConfig.BitRateHigh, AppConfig.VideoResHigh, "-2", false, true, "64k", false, filenamenoext, false}
		defer wg.Done()
	}()

	wg.Wait()
	//create mpd
	videoQuality <- VideoParams{filePath, MPDPath, AppConfig.BitRateHigh, AppConfig.VideoResHigh, "-2", false, false, "64k", true, filenamenoext, false}
	if AppConfig.DelVidAftUpl { // DelVidAftUpl is set true delete the original video
		err := os.Remove(filePath)
		if err != nil {
			fmt.Println(err)
			return
		}
	}
}

func convertVideo(videoQuality chan VideoParams) {
	for params := range videoQuality {
		if params.audio {
			cmd := exec.Command("/usr/bin/ffmpeg", "-i", params.videoPath, "-map_metadata", "-2", "-threads", AppConfig.NrOfCoreVideoConv, "-c:v", "libvpx-vp9", "-b:v", params.quality, "-vf", "scale="+params.width+":"+params.height, params.ConvertPath)
			err := cmd.Run()
			if err != nil {
				fmt.Println("Error converting video:", err)
			}
			fmt.Printf("%s converted to %s resolution %sx%s with audio\n", params.videoPath, params.quality, params.width, params.height)
			quequelen--
		} else if params.createThunb {
			cmd := exec.Command("/usr/bin/ffmpeg", "-i", params.videoPath, "-map_metadata", "-2", "-ss", "00:00:01", "-vframes", "1", "-s", "640x480", "-f", "image2", params.ConvertPath)
			err := cmd.Run()
			if err != nil {
				fmt.Println("Error converting thumbnail:", err)
			}
			fmt.Printf("%s thumbnail created\n", params.videoPath)
			quequelen--

		} else if params.processaudio {
			cmd4 := exec.Command("/usr/bin/ffmpeg", "-i", params.videoPath, "-map_metadata", "-2", "-threads", AppConfig.NrOfCoreVideoConv, "-c:a", "aac", "-b:a", params.audioquality, "-vn", "-f", "mp4", params.ConvertPath)
			err4 := cmd4.Run()
			if err4 != nil {
				fmt.Println(err4)
				file, err := os.Create(AppConfig.ConvertPath + "/" + params.videoName + "/" + params.videoName + "noaudio.txt")
				if err != nil {
					fmt.Println(err)
					return
				}
				defer file.Close()
			}
			fmt.Println("Audio conversion end: ", params.videoName)
			quequelen--
		} else if params.creatempd {
			var outputpath string = filepath.Join(AppConfig.ConvertPath + "/" + params.videoName)
			dashmap := "-dash 2000 -frag 2000 -rap -profile onDemand -out "
			mpdinuput := " " + outputpath + "/high_" + params.videoName + ".mp4#video " + outputpath + "/med_" + params.videoName + ".mp4#video " + outputpath + "/low_" + params.videoName + ".mp4#video "
			if _, err := os.Stat(outputpath + "/" + params.videoName + "noaudio.txt"); os.IsNotExist(err) {
				mpdinuput = mpdinuput + outputpath + "/audio_" + params.videoName + ".mp4#audio "
			}
			input := "MP4Box " + dashmap + params.ConvertPath + mpdinuput
			cmd5 := exec.Command("/bin/sh", "-c", input)

			err5 := cmd5.Run()
			if err5 != nil {
				fmt.Println(err5)
			}
			fmt.Println("MPD creation END ", params.videoName)
			quequelen--
		} else {
			cmd := exec.Command("/usr/bin/ffmpeg", "-i", params.videoPath, "-map_metadata", "-2", "-threads", AppConfig.NrOfCoreVideoConv, "-c:v", "libx264", "-level", "4.1", "-b:v", params.quality, "-g", "60", "-vf", "scale="+params.width+":"+params.height, "-keyint_min", "60", "-sc_threshold", "0", "-an", "-f", "mp4", "-dash", "1", params.ConvertPath)
			err := cmd.Run()
			if err != nil {
				fmt.Println("Error converting video:", err)
			}
			fmt.Printf("%s converted to %s resolution %sx%s\n", params.videoPath, params.quality, params.width, params.height)
			quequelen--
		}
	}
}

func handleSendVideo(w http.ResponseWriter, r *http.Request) {
	p := &PageSndFile{
		UseAuth: AppConfig.AllowUploadWithPsw,
	}
	renderTemplate(w, "sendfile", p)
	return
}

func handleVP(w http.ResponseWriter, r *http.Request) {
	videoname := r.URL.Query().Get("videoname")
	nojs := r.URL.Query().Get("nojs")
	if len(videoname) <= AppConfig.MaxVideoNameLen && isSafeFileName(videoname) {

		if nojs == "1" {
			p := &PageVPNoJS{
				VidNm: videoname,
			}
			renderTemplate(w, "vpnojs", p)
			return
		}

		p := &PageVP{
			VidNm: videoname,
		}
		renderTemplate(w, "vp", p)
		return
	}
	senderror(w, r, "Invalid file name only allowed A-Z,a-z,0-9,-,_ or it'slonger than "+strconv.Itoa(AppConfig.MaxVideoNameLen)+" characters")
}

func handleVideo(w http.ResponseWriter, r *http.Request) {
	videoname := r.URL.Query().Get("videoname")
	speed := r.URL.Query().Get("speed")

	if len(videoname) > AppConfig.MaxVideoNameLen || !isSafeFileName(videoname) {
		senderror(w, r, "Invalid file name only allowed A-Z,a-z,0-9,-,_ or it'slonger than "+strconv.Itoa(AppConfig.MaxVideoNameLen)+" characters")
		return
	}
	//choose the video format based on the connection speed
	var videoName string
	switch speed {
	case "20":
		videoName = "high_" + videoname + ".webm"
	case "10":
		videoName = "med_" + videoname + ".webm"
	default:
		videoName = "low_" + videoname + ".webm"
	}

	//Let's build the path to the video file
	videoPath := filepath.Join("converted/"+videoname, videoName)

	//Let's open the video file
	videoFile, err := os.Open(videoPath)
	if err != nil {
		senderror(w, r, "Error opening video file:"+err.Error())
		return
	}
	defer videoFile.Close()

	//set the content type as video/webm
	w.Header().Set("Content-Type", "video/webm")

	//copy the contents of the video file into the response
	_, err = io.Copy(w, videoFile)
	if err != nil {
		senderror(w, r, "Errore copia file video:"+err.Error())
	}
}

func deleteOLD() {
	for {
		go deleteOldFiles(AppConfig.UploadPath, AppConfig.DaysOld)
		go deleteOldFiles(AppConfig.ConvertPath, AppConfig.DaysOld)
		time.Sleep(checkOldEvery) //wait time before recheck file deletion policies
	}
}

func deleteOldFiles(folderPath string, DaysOld int) {
	files, err := ioutil.ReadDir(folderPath)
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, file := range files {
		if file.IsDir() {
			deleteOldFiles(folderPath+"/"+file.Name(), DaysOld)
			continue
		}

		if time.Since(file.ModTime()).Hours()/24 >= float64(DaysOld) {
			err := os.Remove(folderPath + "/" + file.Name())
			if err != nil {
				fmt.Println(err)
				return
			}
			fmt.Printf("File %s deleted in folder %s.\n", file.Name(), folderPath)
		}
	}
}

func senderror(w http.ResponseWriter, r *http.Request, errormsg string) {
	p := &PageErr{
		ErrMsg: errormsg,
	}
	renderTemplate(w, "error", p)
}

func isSafeFileName(fileName string) bool {
	return safeFileName.MatchString(fileName)
}

func resetVideoUploadedCounter() {
	for {
		// Reset every h
		time.Sleep(time.Hour)
		videosUploaded = 0
	}
}

func ReadConfig() {
	f, err := os.Open(configPath)
	if err != nil {
		fmt.Println(err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&AppConfig)

	if err != nil {
		fmt.Println(err)
	}
}

func renderTemplate(w http.ResponseWriter, tmpl string, p interface{}) {
	var err error
	switch p.(type) {
	case *PageList:
		err = templatefl.ExecuteTemplate(w, tmpl+".html", p)
	case *PageQueque:
		err = templateq.ExecuteTemplate(w, tmpl+".html", p)
	case *PageUploaded:
		err = templateupl.ExecuteTemplate(w, tmpl+".html", p)
	case *PageVP:
		err = templatevp.ExecuteTemplate(w, tmpl+".html", p)
	case *PageVPNoJS:
		err = templatevpnojs.ExecuteTemplate(w, tmpl+".html", p)
	case *PageErr:
		err = templateerr.ExecuteTemplate(w, tmpl+".html", p)
	case *PageSndFile:
		err = templatesndfile.ExecuteTemplate(w, tmpl+".html", p)
	}

	if err != nil {
		fmt.Println("Error renderTemplate: " + err.Error())
		http.Error(w, "Error", http.StatusInternalServerError)
	}
}

func verifyPassword(username string, password string) bool {
	// TODO: Query the database to retrieve the password hash for the specified username
	hashedPassword := AppConfig.Psw

	// Check if the entered password matches the hash of the password in the database
	if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password)); err != nil {
		return false
	}
	return true
}

func (f folderInfos) Len() int {
	return len(f)
}

func (f folderInfos) Less(i, j int) bool {
	return f[i].ModTime.After(f[j].ModTime)
}

func (f folderInfos) Swap(i, j int) {
	f[i], f[j] = f[j], f[i]
}

func listFolders(dirPath string, pageNum int) ([]folderInfo, error) {
	files, err := ioutil.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var infos []folderInfo
	for _, file := range files {
		if file.IsDir() {
			info := folderInfo{
				Name:    file.Name(),
				ModTime: file.ModTime(),
			}
			infos = append(infos, info)
		}
	}

	sort.Sort(folderInfos(infos))

	startIndex := (pageNum - 1) * 10
	endIndex := startIndex + 10
	if endIndex > len(infos) {
		endIndex = len(infos)
	}

	return infos[startIndex:endIndex], nil
}
