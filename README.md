# GoTube

This is a simple video streaming server implemented in Go. The server is designed to handle video uploads in various formats and provides functionalities like video conversion and deletion of older files. The server configuration is stored in a YAML file, which can be easily modified to fit the user's needs.

## Features

    Secure file uploads with the option to enable TLS
    Optional Password protection for video upload
    Limit the number of videos uploaded per hour
    Video conversion to different resolutions and formats (DASH and WebM)
    Ability to delete old files after a specified number of days
    Simple and intuitive web interface
    HTML templates for displaying file lists, upload progress, and error messages
    Video conversion with customizable resolution and quality settings
    Removal of metadata to enhance the privacy of uploaded videos.


## Configuration

The server's configuration is stored in a YAML file (config.yaml), which can be modified to fit the user's needs. The following options are available:

    EnableTLS: Enable/disable TLS support
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
    AllowUploadWithPsw: Allow video upload only with password
    Psw: Password for video upload. Generate With "mkpasswd -m bcrypt -R 10 <password>"
    NrOfCoreVideoConv: Number of threads used for video conversion


## Usage  
To get started, you need to have Go installed on your machine. Then, you can clone the repository to your local machine and run the server.  

    Modify the config.yaml file to fit your needs
    Run the code with the following command: go run main.go
    Access the file upload page at http://<server-ip>:<port>/ or https://<server-ip>:<port>/ (if TLS is enabled)
    
Optionally, you can disable TLS and bind the server to "127.0.0.1" so that it is only accessible from localhost then expose it as an onion service through TOR.  


## Contribution

Contributions to this project are always welcome. If you have any new ideas or suggestions for improvement, please feel free to open a new issue or pull request.  
  
## License

This project is licensed under the GPL-3 License.
