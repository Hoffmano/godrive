# Godrive

Did you tried to download your hole Google Drive direct to an external HD
This isn't an easily task
This golang script connect direct with Google Drive and download your files without need to zip into a bunch o zipped files

1. Clone this repository
2. Configure your environment and get credentials: <https://developers.google.com/workspace/drive/api/quickstart/go?hl=pt-br>
3. Edit main.go constants

```
const (
 numWorkers      = 1000 // quantity of files being downloaded in parallel
 downloadPath    = "/media/ghs/hd/godrive2/" // your target path
 driveFolderPath = "" // empty to download Google Drive root folder
)
```
