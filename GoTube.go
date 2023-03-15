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
	AllowEmbedded      bool   `yaml:"AllowEmbedded"`
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
	VideoConvPreset    string `yaml:"VideoConvPreset"`
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
	templatevpemb       = template.Must(template.ParseFiles("pages/embedded.html"))
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
type PageVPEMB struct {
	VidNm string
}
type PageVP struct {
	VidNm string
	Embed bool
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
	http.HandleFunc("/vp", handleVP)
	http.HandleFunc("/Send", handleSendVideo)
	http.HandleFunc("/", http.HandlerFunc(listFolderHandler))
	http.Handle("/static/", http.StripPrefix("/static", http.FileServer(http.Dir(staticPath))))
	http.Handle("/converted/", http.StripPrefix("/converted", http.FileServer(http.Dir("./converted"))))
	http.HandleFunc("/favicon.ico", http.HandlerFunc(faviconHandler))
	http.HandleFunc("/lst", listFolderHandler)
	http.HandleFunc("/queque", quequeSize)
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

func quequeSize(w http.ResponseWriter, r *http.Request) {
	p := &PageQueque{
		QuequeSize: quequelen,
	}
	renderTemplate(w, "queque", p)
}

func listFolderHandler(w http.ResponseWriter, r *http.Request) {
	pageNum, err := strconv.Atoi(r.FormValue("page"))
	if err != nil || pageNum < 1 {
		pageNum = 1
	}

	dirPath := "converted"

	folders, err := listFolders(dirPath, pageNum)
	if err != nil {
		sendError(w, r, err.Error())
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

	data.TotalPage = (len(folders) + (AppConfig.VideoPerPage - 1)) / AppConfig.VideoPerPage

	renderTemplate(w, "filelist", data)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("video")
	username := r.FormValue("username")
	password := r.FormValue("password")

	if AppConfig.AllowUploadWithPsw && !verifyPassword(username, password) {
		time.Sleep(5000)
		sendError(w, r, "Wrong password")
		return
	}

	var errormsg string
	if err != nil {
		sendError(w, r, err.Error())
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
		sendError(w, r, errormsg)
		return
	}

	extension := path.Ext(filename)
	filenamenoext := strings.TrimSuffix(filename, extension)
	filePath := filepath.Join(AppConfig.UploadPath, filename)
	// Check if the file already exists
	if _, err := os.Stat(filepath.Clean(filePath)); !os.IsNotExist(err) {
		errormsg = "File already exists: " + filename
		sendError(w, r, errormsg)
		return
	}

	out, err := os.Create(filepath.Clean(filePath))
	if err != nil {
		sendError(w, r, err.Error())
		return
	}
	defer out.Close()

	videosUploaded++

	_, err = io.Copy(out, file)
	if err != nil {
		sendError(w, r, err.Error())
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

func StartconvertVideo(filePath, ConvertPath, filenamenoext string) {
	convertedBasePath := filepath.Join(ConvertPath, filenamenoext)
	dirPath := filepath.Join(ConvertPath, filenamenoext)

	err := os.Mkdir(filepath.Clean(dirPath), 0755)
	if err != nil {
		fmt.Println(err)
		return
	}
	quequelen += 7

	if !channelOpen {
		go convertVideo(videoQuality)
		channelOpen = true
	}

	var wglowqualityconv, wg sync.WaitGroup
	wglowqualityconv.Add(2)
	wg.Add(4)

	launchConversion := func(params VideoParams, wg *sync.WaitGroup) {
		videoQuality <- params
		wg.Done()
	}

	go launchConversion(VideoParams{filePath, filepath.Join(convertedBasePath, "low_"+filenamenoext+"_audio.webm"), AppConfig.BitRateLow, AppConfig.VideoResLow, "-2", true, false, "64k", false, filenamenoext, false}, &wglowqualityconv)
	go launchConversion(VideoParams{filePath, filepath.Join(convertedBasePath, "output.jpeg"), AppConfig.BitRateHigh, AppConfig.VideoResHigh, "-2", false, false, "64k", false, filenamenoext, true}, &wglowqualityconv)
	wglowqualityconv.Wait()

	go launchConversion(VideoParams{filePath, filepath.Join(convertedBasePath, "low_"+filenamenoext+".mp4"), AppConfig.BitRateLow, AppConfig.VideoResLow, "-2", false, false, "64k", false, filenamenoext, false}, &wg)
	go launchConversion(VideoParams{filePath, filepath.Join(convertedBasePath, "med_"+filenamenoext+".mp4"), AppConfig.BitRateMed, AppConfig.VideoResMed, "-2", false, false, "64k", false, filenamenoext, false}, &wg)
	go launchConversion(VideoParams{filePath, filepath.Join(convertedBasePath, "high_"+filenamenoext+".mp4"), AppConfig.BitRateHigh, AppConfig.VideoResHigh, "-2", false, false, "64k", false, filenamenoext, false}, &wg)
	go launchConversion(VideoParams{filePath, filepath.Join(convertedBasePath, "audio_"+filenamenoext+".mp4"), AppConfig.BitRateHigh, AppConfig.VideoResHigh, "-2", false, true, "64k", false, filenamenoext, false}, &wg)
	wg.Wait()

	videoQuality <- VideoParams{filePath, filepath.Join(convertedBasePath, "output.mpd"), AppConfig.BitRateHigh, AppConfig.VideoResHigh, "-2", false, false, "64k", true, filenamenoext, false}

	if AppConfig.DelVidAftUpl {
		err := os.Remove(filePath)
		if err != nil {
			fmt.Println("error removing original video file:", err)
		}
	}
}

func convertVideo(videoQuality chan VideoParams) {
	runCommand := func(cmd *exec.Cmd, description string) {
		err := cmd.Run()
		if err != nil {
			fmt.Printf("Error %s: %v\n", description, err)
		} else {
			fmt.Println(description)
		}
		quequelen--
	}

	createMPD := func(params VideoParams) {
		outputPath := filepath.Join(AppConfig.ConvertPath, params.videoName)
		dashmap := "-dash 2000 -frag 2000 -rap -profile onDemand -out "
		mpdinuput := " " + outputPath + "/high_" + params.videoName + ".mp4#video " + outputPath + "/med_" + params.videoName + ".mp4#video " + outputPath + "/low_" + params.videoName + ".mp4#video "
		noAudioFilePath := filepath.Join(outputPath, params.videoName+"noaudio.txt")
		if _, err := os.Stat(filepath.Clean(noAudioFilePath)); os.IsNotExist(err) {
			mpdinuput = mpdinuput + outputPath + "/audio_" + params.videoName + ".mp4#audio "
		}
		input := "MP4Box " + dashmap + params.ConvertPath + mpdinuput
		cmd := exec.Command("/bin/sh", "-c", input)

		err := cmd.Run()
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println("MPD creation END ", params.videoName)
		files := []string{
			filepath.Join(outputPath, "low_"+params.videoName+".mp4"),
			filepath.Join(outputPath, "med_"+params.videoName+".mp4"),
			filepath.Join(outputPath, "high_"+params.videoName+".mp4"),
			filepath.Join(outputPath, "audio_"+params.videoName+".mp4"),
		}
		for _, f := range files {
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				fmt.Printf("error removing file %s: %v\n", f, err)
			}
		}
		quequelen--
	}
	
	for params := range videoQuality {
		if params.audio {
			cmd := exec.Command("/usr/bin/ffmpeg", "-i", params.videoPath, "-map_metadata", "-2", "-threads", AppConfig.NrOfCoreVideoConv, "-c:v", "libvpx-vp9", "-b:v", params.quality, "-vf", "scale="+params.width+":"+params.height, params.ConvertPath)
			runCommand(cmd, fmt.Sprintf("%s converted to %s resolution %sx%s with audio", params.videoPath, params.quality, params.width, params.height))
		} else if params.createThunb {
			cmd := exec.Command("/usr/bin/ffmpeg", "-i", params.videoPath, "-map_metadata", "-2", "-ss", "00:00:01", "-vframes", "1", "-s", "640x480", "-f", "image2", params.ConvertPath)
			runCommand(cmd, fmt.Sprintf("%s thumbnail created", params.videoPath))
		} else if params.processaudio {
			cmd := exec.Command("/usr/bin/ffmpeg", "-i", params.videoPath, "-map_metadata", "-2", "-threads", AppConfig.NrOfCoreVideoConv, "-c:a", "aac", "-b:a", params.audioquality, "-vn", "-f", "mp4", params.ConvertPath)
			err := cmd.Run()
			if err != nil {
				fmt.Println(err)
				noAudioFilePath := filepath.Join(AppConfig.ConvertPath, params.videoName, params.videoName+"noaudio.txt")
				file, err := os.Create(noAudioFilePath)
				if err != nil {
					fmt.Println(err)
					return
				}
				defer file.Close()
			}
			fmt.Println("Audio conversion end: ", params.videoName)
			quequelen--
		} else if params.creatempd {
			createMPD(params)
		} else {
			cmd := exec.Command("/usr/bin/ffmpeg", "-i", params.videoPath, "-map_metadata", "-2", "-threads", AppConfig.NrOfCoreVideoConv, "-c:v", "libx264", "-level", "4.1", "-b:v", params.quality, "-g", "60", "-vf", "scale="+params.width+":"+params.height, "-preset", AppConfig.VideoConvPreset, "-keyint_min", "60", "-sc_threshold", "0", "-an", "-f", "mp4", "-dash", "1", params.ConvertPath)
			runCommand(cmd, fmt.Sprintf("%s converted to %s resolution %sx%s", params.videoPath, params.quality, params.width, params.height))
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
	emb := r.URL.Query().Get("embedded")

	if len(videoname) <= AppConfig.MaxVideoNameLen && isSafeFileName(videoname) {
		if emb == "1" && AppConfig.AllowEmbedded {
			p := &PageVPEMB{
				VidNm: videoname,
			}
			renderTemplate(w, "embedded", p)
			return
		}
		if nojs == "1" {
			p := &PageVPNoJS{
				VidNm: videoname,
			}
			renderTemplate(w, "vpnojs", p)
			return
		}

		p := &PageVP{
			VidNm: videoname,
			Embed: AppConfig.AllowEmbedded,
		}
		renderTemplate(w, "vp", p)
		return
	}
	sendError(w, r, "Invalid file name only allowed A-Z,a-z,0-9,-,_ or it'slonger than "+strconv.Itoa(AppConfig.MaxVideoNameLen)+" characters")
}

func deleteOLD() {
	for {
		go deleteOldFiles(AppConfig.UploadPath, AppConfig.DaysOld)
		go deleteOldFiles(AppConfig.ConvertPath, AppConfig.DaysOld)
		time.Sleep(checkOldEvery) //wait time before recheck file deletion policies
	}
}

func deleteOldFiles(folderPath string, daysOld int) {
	files, err := ioutil.ReadDir(folderPath)
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, file := range files {
		filePath := filepath.Join(folderPath, file.Name())

		if file.IsDir() {
			deleteOldFiles(filePath, daysOld)
			continue
		}

		if time.Since(file.ModTime()).Hours()/24 >= float64(daysOld) {
			if err := os.Remove(filePath); err != nil {
				fmt.Println(err)
				return
			}
			fmt.Printf("File %s deleted in folder %s.\n", file.Name(), folderPath)
		}
	}
}

func sendError(w http.ResponseWriter, r *http.Request, errormsg string) {
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
	case *PageVPEMB:
		err = templatevpemb.ExecuteTemplate(w, tmpl+".html", p)
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

func verifyPassword(username, password string) bool {
	// TODO: Query the database to retrieve the password hash for the specified username
	hashedPassword := AppConfig.Psw
	// Check if the entered password matches the hash of the password in the database
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
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
	if startIndex >= len(infos) {
		if startIndex == 0 {
			return nil, fmt.Errorf("No video available.")
		}
		return nil, fmt.Errorf("Invalid page number: %d", pageNum)
	}
	endIndex := startIndex + 10
	if endIndex > len(infos) {
		endIndex = len(infos)
	}

	return infos[startIndex:endIndex], nil
}
