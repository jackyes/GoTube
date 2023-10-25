package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	"sync/atomic"
	"text/template"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

type Cfg struct {
	EnableTLS                 bool   `yaml:"EnableTLS"`
	EnableNoTLS               bool   `yaml:"EnableNoTLS"`
	EnableFDP                 bool   `yaml:"EnableFDP"`
	EnablePHL                 bool   `yaml:"EnablePHL"`
	AllowEmbedded             bool   `yaml:"AllowEmbedded"`
	MaxUploadSize             int64  `yaml:"MaxUploadSize"`
	DaysOld                   int    `yaml:"DaysOld"`
	DelVidAftUpl              bool   `yaml:"DelVidAftUpl"`
	CertPathCrt               string `yaml:"CertPathCrt"`
	CertPathKey               string `yaml:"CertPathKey"`
	ServerPort                string `yaml:"ServerPort"`
	ServerPortTLS             string `yaml:"ServerPortTLS"`
	BindtoAdress              string `yaml:"BindtoAdress"`
	MaxVideosPerHour          int    `yaml:"MaxVideosPerHour"`
	VideoPerPage              int    `yaml:"VideoPerPage"`
	MaxVideoNameLen           int    `yaml:"MaxVideoNameLen"`
	VideoResLow               string `yaml:"VideoResLow"`
	VideoResMed               string `yaml:"VideoResMed"`
	VideoResHigh              string `yaml:"VideoResHigh"`
	BitRateLow                string `yaml:"BitRateLow"`
	BitRateMed                string `yaml:"BitRateMed"`
	BitRateHigh               string `yaml:"BitRateHigh"`
	UploadPath                string `yaml:"UploadPath"`
	ConvertPath               string `yaml:"ConvertPath"`
	CheckOldEvery             string `yaml:"CheckOldEvery"`
	AllowUploadOnlyFromUsers  bool   `yaml:"AllowUploadOnlyFromUsers"`
	VideoOnlyForUsers         bool   `yaml:"VideoOnlyForUsers"`
	NrOfCoreVideoConv         string `yaml:"NrOfCoreVideoConv"`
	VideoConvPreset           string `yaml:"VideoConvPreset"`
	AllowUploadOnlyFromAdmins bool   `yaml:"AllowUploadOnlyFromAdmins"`
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
	templateConfig      = template.Must(template.ParseFiles("pages/editconfig.html"))
	videoQuality        = make(chan VideoParams)
	channelOpen         = false
	users           []User
	cookieKeys      [][]byte // Array of secret keys for key rotation
	currentKeyIndex int      // Index of the current secret key
)

const (
	configPath      = "./config.yaml"
	staticPath      = "./static"
	faviconPath     = "./static/favicon.ico"
	sendfilePath    = "./static/sendfile.html"
	sessionIDLength = 32
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

type User struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Role     string `yaml:"role"`
}

type PageList struct {
	Files     []folderInfo
	PrevPage  int
	NextPage  int
	TotalPage int
	CanDelete int
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
	// Read YAML user file
	data, err := ioutil.ReadFile("users.yaml")
	if err != nil {
		panic(err)
	}

	// Parse YAML user file
	err = yaml.Unmarshal(data, &users)
	if err != nil {
		panic(err)
	}

	for i := 0; i < 3; i++ { // Generates 3 secret keys for key rotation
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			panic(err)
		}
		cookieKeys = append(cookieKeys, key)
	}
	currentKeyIndex = 0

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
	http.HandleFunc("/deleteVideo", handleDeleteVideo)
	http.HandleFunc("/", http.HandlerFunc(listFolderHandler))
	http.Handle("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if AppConfig.VideoOnlyForUsers {
			if !adminAuthenticated(r) && !userAuthenticated(r) {
				http.Redirect(w, r, "/auth", http.StatusSeeOther)
				return
			}
		}
		http.StripPrefix("/static", http.FileServer(http.Dir(staticPath))).ServeHTTP(w, r)
	}))

	http.Handle("/converted/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if AppConfig.VideoOnlyForUsers {
			if !adminAuthenticated(r) && !userAuthenticated(r) {
				http.Redirect(w, r, "/auth", http.StatusSeeOther)
				return
			}
		}
		http.StripPrefix("/converted", http.FileServer(http.Dir("./converted"))).ServeHTTP(w, r)
	}))

	http.HandleFunc("/favicon.ico", http.HandlerFunc(faviconHandler))
	http.HandleFunc("/lst", listFolderHandler)
	http.HandleFunc("/queque", quequeSize)
	http.HandleFunc("/editconfig", editConfigHandler)
	http.HandleFunc("/save-config", saveConfigHandler)
	http.HandleFunc("/auth", loginHandler)
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

func loginHandler(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	// Find the user with the given username
	var currentUser User
	for _, user := range users {
		if user.Username == username {
			currentUser = user
			break
		}
	}
	// Verify the password
	err := bcrypt.CompareHashAndPassword([]byte(currentUser.Password), []byte(password))
	if err == nil {
		// Authentication succeeded: set the cookie and redirect to the welcome page
		expiration := time.Now().Add(24 * time.Hour)
		value := currentUser.Username + "|" + currentUser.Role
		cookie := createSignedCookie("auth", value, expiration)
		http.SetCookie(w, cookie)
		// Redirect the user to the home page
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	// Passwords don't match, show an error message
	sendError(w, r, "Invalid username or password")
}

func editConfigHandler(w http.ResponseWriter, r *http.Request) {
	if !adminAuthenticated(r) {
		http.Redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}

	configMap := structToMap(&AppConfig)
	if err := templateConfig.Execute(w, configMap); err != nil {
		sendError(w, r, "Error during template generation")
		return
	}
}

func structToMap(config *Cfg) map[string]interface{} {
	// Marshal the config struct to JSON
	jsonData, err := json.Marshal(config)
	if err != nil {
		return nil
	}
	// Decode the JSON data into a map
	var configMap map[string]interface{}
	err = json.Unmarshal(jsonData, &configMap)
	if err != nil {
		return nil
	}

	// Convert MaxUploadSize to a normal string representation
	configMap["MaxUploadSize"] = strconv.FormatInt(config.MaxUploadSize, 10)

	return configMap
}

func saveConfigHandler(w http.ResponseWriter, r *http.Request) {
	if !adminAuthenticated(r) {
		http.Redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		sendError(w, r, "Error while processing the form")
		return
	}

	configMap := make(map[string]interface{})
	for key, values := range r.PostForm {
		value := values[0]
		configMap[key] = value
	}

	config := mapToStruct(configMap)
	if err := saveConfig("config.yaml", config); err != nil {
		sendError(w, r, "Error while saving the configuration file")
		return
	}
	AppConfig = *config
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func saveConfig(configPath string, config *Cfg) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	err = ioutil.WriteFile(configPath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func mapToStruct(configMap map[string]interface{}) *Cfg {
	config := &Cfg{}
	for key, value := range configMap {
		switch key {
		case "EnableTLS":
			config.EnableTLS, _ = strconv.ParseBool(value.(string))
		case "EnableNoTLS":
			config.EnableNoTLS, _ = strconv.ParseBool(value.(string))
		case "MaxUploadSize":
			config.MaxUploadSize, _ = strconv.ParseInt(value.(string), 10, 64)
		case "DaysOld":
			config.DaysOld, _ = strconv.Atoi(value.(string))
		case "ServerPortTLS":
			config.ServerPortTLS = value.(string)
		case "ServerPort":
			config.ServerPort = value.(string)
		case "CertPathCrt":
			config.CertPathCrt = value.(string)
		case "CertPathKey":
			config.CertPathKey = value.(string)
		case "BindtoAdress":
			config.BindtoAdress = value.(string)
		case "MaxVideosPerHour":
			config.MaxVideosPerHour, _ = strconv.Atoi(value.(string))
		case "MaxVideoNameLen":
			config.MaxVideoNameLen, _ = strconv.Atoi(value.(string))
		case "VideoResLow":
			config.VideoResLow = value.(string)
		case "VideoResMed":
			config.VideoResMed = value.(string)
		case "VideoResHigh":
			config.VideoResHigh = value.(string)
		case "BitRateLow":
			config.BitRateLow = value.(string)
		case "BitRateMed":
			config.BitRateMed = value.(string)
		case "BitRateHigh":
			config.BitRateHigh = value.(string)
		case "EnableFDP":
			config.EnableFDP, _ = strconv.ParseBool(value.(string))
		case "EnablePHL":
			config.EnablePHL, _ = strconv.ParseBool(value.(string))
		case "UploadPath":
			config.UploadPath = value.(string)
		case "ConvertPath":
			config.ConvertPath = value.(string)
		case "CheckOldEvery":
			config.CheckOldEvery = value.(string)
		case "AllowUploadOnlyFromUsers":
			config.AllowUploadOnlyFromUsers, _ = strconv.ParseBool(value.(string))
		case "AllowUploadOnlyFromAdmins":
			config.AllowUploadOnlyFromAdmins, _ = strconv.ParseBool(value.(string))
		case "VideoOnlyForUsers":
			config.VideoOnlyForUsers, _ = strconv.ParseBool(value.(string))
		case "NrOfCoreVideoConv":
			config.NrOfCoreVideoConv = value.(string)
		case "DelVidAftUpl":
			config.DelVidAftUpl, _ = strconv.ParseBool(value.(string))
		case "VideoPerPage":
			config.VideoPerPage, _ = strconv.Atoi(value.(string))
		case "VideoConvPreset":
			config.VideoConvPreset = value.(string)
		case "AllowEmbedded":
			config.AllowEmbedded, _ = strconv.ParseBool(value.(string))
		}
	}
	return config
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

func userAuthenticated(r *http.Request) bool {
	for i := 0; i < len(cookieKeys); i++ {
		if hasRoleWithKey(r, "user", i) {
			// The user has the "user" role
			return true
		}
	}
	// The user does not have the required role
	return false
}
func adminAuthenticated(r *http.Request) bool {
	for i := 0; i < len(cookieKeys); i++ {
		if hasRoleWithKey(r, "admin", i) {
			// The user has the "admin" role
			return true
		}
	}
	// The user does not have the required role
	return false
}

func listFolderHandler(w http.ResponseWriter, r *http.Request) {
	if AppConfig.VideoOnlyForUsers && !adminAuthenticated(r) && !userAuthenticated(r) {
		http.Redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}

	pageNum := 1
	if page, err := strconv.Atoi(r.FormValue("page")); err == nil && page > 0 {
		pageNum = page
	}

	const dirPath = "converted"
	folders, err := listFolders(dirPath, pageNum)
	if err != nil {
		sendError(w, r, err.Error())
		return
	}

	data := &PageList{
		Files:     folders,
		TotalPage: (len(folders) + (AppConfig.VideoPerPage - 1)) / AppConfig.VideoPerPage,
	}

	if pageNum > 1 {
		data.PrevPage = pageNum - 1
	}

	if len(folders) == AppConfig.VideoPerPage {
		data.NextPage = pageNum + 1
	}
	if adminAuthenticated(r) {
		data.CanDelete = 1
	}

	renderTemplate(w, "filelist", data)
}

func handleDeleteVideo(w http.ResponseWriter, r *http.Request) {
	if !adminAuthenticated(r) {
		http.Redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}
	videoname := r.URL.Query().Get("videoname")
	err := os.RemoveAll(filepath.Join(AppConfig.ConvertPath, videoname))
	if err != nil {
		sendError(w, r, err.Error())
		return
	}

}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if AppConfig.AllowUploadOnlyFromAdmins {
		if !adminAuthenticated(r) {
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
	}
	if AppConfig.AllowUploadOnlyFromUsers {
		if !adminAuthenticated(r) && !userAuthenticated(r) {
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
	}
	file, header, err := r.FormFile("video")

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

	// Sanitize the file path
	filePath := filepath.Join(AppConfig.UploadPath, filepath.Clean(filename))
	if !strings.HasPrefix(filePath, AppConfig.UploadPath) {
		errormsg = "Invalid file name"
		sendError(w, r, errormsg)
		return
	}

	// Check if the file already exists
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		errormsg = "File already exists: " + filename
		sendError(w, r, errormsg)
		return
	}

	out, err := os.Create(filePath)
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
		dashMap := "-dash 2000 -frag 2000 -rap -profile onDemand -out "
		mpdInuput := " " + outputPath + "/high_" + params.videoName + ".mp4#video " + outputPath + "/med_" + params.videoName + ".mp4#video " + outputPath + "/low_" + params.videoName + ".mp4#video "
		noAudioFilePath := filepath.Join(outputPath, params.videoName+"noaudio.txt")
		if _, err := os.Stat(filepath.Clean(noAudioFilePath)); os.IsNotExist(err) {
			mpdInuput = mpdInuput + outputPath + "/audio_" + params.videoName + ".mp4#audio "
		}
		input := "MP4Box " + dashMap + params.ConvertPath + mpdInuput
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
				file, err := os.Create(filepath.Clean(noAudioFilePath))
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
	if AppConfig.AllowUploadOnlyFromAdmins {
		if !adminAuthenticated(r) {
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
	}
	if AppConfig.AllowUploadOnlyFromUsers {
		if !adminAuthenticated(r) && !userAuthenticated(r) {
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
	}
	p := &PageSndFile{
		UseAuth: AppConfig.AllowUploadOnlyFromUsers, //TODO: REMOVE TEMPLATE, use static page
	}
	renderTemplate(w, "sendfile", p)
	return
}

func handleVP(w http.ResponseWriter, r *http.Request) {
	if AppConfig.VideoOnlyForUsers {
		if !adminAuthenticated(r) && !userAuthenticated(r) {
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
	}
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

func deleteOldFiles(folderPath string, daysOld int) error {
	err := filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if time.Since(info.ModTime()).Hours()/24 >= float64(daysOld) {
				if err := os.RemoveAll(path); err != nil {
					return err
				}
				fmt.Printf("Folder %q deleted.\n", path)
				return filepath.SkipDir
			}
			return nil
		}

		if time.Since(info.ModTime()).Hours()/24 >= float64(daysOld) {
			if err := os.Remove(path); err != nil {
				return err
			}
			fmt.Printf("File %q deleted in folder %q.\n", info.Name(), folderPath)
		}
		return nil
	})
	return err
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
	// Create an atomic integer to store the counter
	var videosUploaded atomic.Int64

	// Start a goroutine to reset the counter every hour
	go func() {
		for range time.NewTicker(time.Hour).C {
			videosUploaded.Store(0)
		}
	}()

	// Wait for the goroutine to finish
	time.Sleep(time.Hour)
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

func hasRoleWithKey(r *http.Request, role string, keyIndex int) bool {
	cookie, err := r.Cookie("auth")
	if err != nil {
		// Cookie not found, assume the user is not authenticated
		return false
	}

	value, err := verifySignedCookieWithKey("auth", cookie.Value, cookieKeys[keyIndex])
	if err != nil {
		// Invalid cookie signature or format, assume the user is not authenticated
		return false
	}

	parts := strings.Split(value, "|")
	if len(parts) != 2 {
		fmt.Println("3")
		// Invalid cookie format, assume the user is not authenticated

		return false
	}

	username := parts[0]
	roleValue := parts[1]

	// Verify that the user has the required role
	for _, user := range users {
		if user.Username == username && user.Role == roleValue {
			return user.Role == role
		}
	}

	// The user was not found, assume the user is not authenticated
	return false
}

func createSignedCookie(name, value string, expires time.Time) *http.Cookie {
	// Select the current cookie key
	cookieKey := cookieKeys[currentKeyIndex]
	// Encode the cookie value and sign it with the cookie key
	cookieValue := value + "|" + expires.Format(time.RFC3339)
	signature := computeCookieSignature(name, cookieValue, cookieKey)

	cookieValueBase64 := base64.StdEncoding.EncodeToString([]byte(cookieValue))
	signatureBase64 := base64.StdEncoding.EncodeToString(signature)

	// Create the cookie with the encoded value and signature
	cookie := &http.Cookie{
		Name:     name,
		Value:    cookieValueBase64 + "|" + signatureBase64,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		Expires:  expires,
	}
	return cookie
}

func verifySignedCookieWithKey(name, value string, key []byte) (string, error) {
	// Decode the cookie value and signature from base64
	parts := strings.Split(value, "|")
	if len(parts) != 2 {
		return "", errors.New("Invalid cookie format")
	}
	cookieValueBase64 := parts[0]
	signatureBase64 := parts[1]
	cookieValue, err := base64.StdEncoding.DecodeString(cookieValueBase64)
	if err != nil {
		return "", errors.New("Invalid cookie format")
	}

	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return "", errors.New("Invalid cookie format")
	}

	// Verify the signature of the cookie using the specified key
	if !validateCookieSignature(name, string(cookieValue), signature, key) {
		return "", errors.New("Invalid cookie signature")
	}

	// Check if the cookie has expired
	parts = strings.Split(string(cookieValue), "|")
	if len(parts) != 3 {
		return "", errors.New("Invalid cookie format")
	}
	if err != nil {
		fmt.Println("Error decoding base64 string:", err)
		return "", errors.New("Invalid cookie format")
	}
	expiration, err := time.Parse(time.RFC3339, string(string(parts[2])))
	if err != nil {
		return "", errors.New("Invalid cookie format")
	}
	if time.Now().After(expiration) {
		return "", errors.New("Cookie has expired")
	}
	// Return the cookie value
	return string(parts[0] + "|" + parts[1]), nil
}

func computeCookieSignature(name, value string, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(name))
	h.Write([]byte("|"))
	h.Write([]byte(value))
	return h.Sum(nil)
}

func validateCookieSignature(name, value string, signature []byte, key []byte) bool {
	expectedSignature := computeCookieSignature(name, value, key)
	return hmac.Equal(signature, expectedSignature)
}
