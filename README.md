# GoTube

This is a simple video streaming server implemented in Go. The server is designed to handle video uploads in various formats and provides functionalities like video conversion and deletion of older files. The server configuration is stored in a YAML file, which can be easily modified to fit the user's needs.
  
## Demo  
[[Onion link] http://gotubehspso2axefhycyei4cr3buo7rcotszpa6pmea27o6y33q7egqd.onion/ (Use Tor Browser)](http://gotubehspso2axefhycyei4cr3buo7rcotszpa6pmea27o6y33q7egqd.onion/)
  
## Features

    Secure file uploads with the option to enable TLS
    Optional Password protection for video upload
    List uploaded videos and choose the number of videos displayed per page
    Limit the number of videos uploaded per hour
    Video conversion to different resolutions and formats (DASH and WebM)
    Ability to delete old files after a specified number of days
    Ability to delete original video after conversion
    Simple and intuitive web interface
    HTML templates for displaying file lists, upload progress, and error messages
    Video conversion with customizable resolution and quality settings
    Removal of metadata to enhance the privacy of uploaded videos.
    Easy sharing on other website
    Limit video upload to admins or admins/users
    Limit video view to users
    


## Configuration
The admin and user username/password are stored in a YAML file (users.yaml).  
The server's configuration is stored in a YAML file (config.yaml), which can be modified to fit the user's needs. The following options are available:

    EnableTLS: Enable/disable TLS support
    EnableNoTLS: Enable/disable Http without TLS
    EnableFDP: Enable/disable automatic deletion of old files
    EnablePHL: Enable/disable file name validation
    MaxUploadSize: Maximum file size allowed for uploads
    DaysOld: Number of days before a file is considered old and eligible for deletion
    CertPathCrt: Path to the SSL certificate
    CertPathKey: Path to the SSL private key
    ServerPort: Port for the HTTP server
    ServerPortTLS: Port for the HTTPS server
    BindtoAdress: IP address to bind the server to
    MaxVideosPerHour: Maximum number of video conversions allowed per hour
    MaxVideoNameLen: Maximum length of a video file name
    VideoResLow: Low video resolution
    VideoResMed: Medium video resolution
    VideoResHigh: High video resolution
    CrfLow: Low video quality
    CrfMed: Medium video quality
    CrfHigh: High video quality
    UploadPath: Path to the uploaded file directory
    ConvertPath: Path to the converted video directory
    AllowUploadOnlyFromUsers: Allow upload only from users and admins
    AllowUploadOnlyFromAdmins: Allow upload only from users and admins
    VideoOnlyForUsers: Show video and list only to users and admin
    NrOfCoreVideoConv: Number of threads used for video conversion
    DelVidAftUpl: Delete or keep original video after conversion
    VideoPerPage: Number of displayed video per page in Video list
    VideoConvPreset: Preset userd for conversion. Options: ultrafast, superfast, veryfast, faster, fast, medium, slow, slower, veryslow
    AllowEmbedded: Allow page for Embedding video in other page




## Usage  
To get started, you need to have Go installed on your machine. Then, you can clone the repository to your local machine and run the server.  

    Modify the config.yaml file to fit your needs
    Modify users.yaml
    Run the code with the following command: go run main.go
    Access the file upload page at http://<server-ip>:<port>/ or https://<server-ip>:<port>/ (if TLS is enabled)
    
Optionally, you can disable TLS and bind the server to "127.0.0.1" so that it is only accessible from localhost then expose it as an onion service through TOR.  
  
## Docker  

It is possible to use an image on Docker Hub with the following command:

    docker run -p 8085:8085 --name gotube -v /home/user/users.yaml:/users.yaml -v /home/user/config.yaml:/config.yaml -v /home/user/uploads:/uploads -v /home/user/converted:/converted jackyes/gotube  
    
`/home/user/config.yaml` is the path to your config.yaml file (copy and edit the one in this repository).  
`/home/user/users.yaml` is the path to admin user/password config file users.yaml (copy and edit the one in this repository)  
`/home/user/uploads` is the folder where uploaded videos will be stored.  
`/home/user/converted` is the folder where the uploaded videos will be converted.  
change the default port 8085 accordingly with the one in config.yaml if you modify it.
  
### Build Docker image yourself  
It is possible to create a Docker container following these steps:  
Clone the repository  

    git clone https://github.com/jackyes/GoTube.git  
    
Edit the config.yaml file  
  
    cd GoTube
    nano config.yaml
  
Create the Docker container  
  
    docker build -t gotube .  
  
Run the container  
  
    docker run -p 8085:8085 gotube  
  
## Prerequisite  
[FFMpeg](https://ffmpeg.org/)  
[MP4Box](https://github.com/gpac/gpac/wiki/MP4Box) 
  
## Contribution

Contributions to this project are always welcome. If you have any new ideas or suggestions for improvement, please feel free to open a new issue or pull request.  
  
## License

This project is licensed under the GPL-3 License.
