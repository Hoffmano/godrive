package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const (
	numWorkers      = 1000
	downloadPath    = "/media/ghs/hd/godrive2/"
	driveFolderPath = "drive"
)

var (
	skippedLog *log.Logger
	errorLog   *log.Logger
)

type fileJob struct {
	file      *drive.File
	localPath string
}

type statusTracker struct {
	totalFilesFound     atomic.Int32
	completedFiles      atomic.Int32
	skippedFiles        atomic.Int32
	isDiscoveryFinished atomic.Bool
	startTime           time.Time
}

func init() {
	skippedFile, err := os.OpenFile("skipped.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal("Failed to open info log file:", err)
	}

	skippedLog = log.New(skippedFile, "", log.Ldate|log.Ltime|log.Lshortfile)

	errorFile, err := os.OpenFile("error.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal("Failed to open error log file:", err)
	}
	errorLog = log.New(errorFile, "", log.Ldate|log.Ltime|log.Lshortfile)
}

func main() {
	context := context.Background()
	driveService := authenticate(context)

	fmt.Printf("Resolvendo o caminho da pasta do Drive: '%s'\n", driveFolderPath)
	folderID, error := getDriveFolderIDByPath(driveService, driveFolderPath)
	if error != nil {
		log.Println("ERRO: %v", error)
	}

	channelFileJob := make(chan *fileJob, 200000)
	var downloadWaitGroup sync.WaitGroup
	var discoveryWaitGroup sync.WaitGroup

	statusTracker := statusTracker{startTime: time.Now()}

	channelIsDone := make(chan bool)
	go printStatus(&statusTracker, channelIsDone)

	for workerID := 1; workerID <= numWorkers; workerID++ {
		go startDownloadWorker(workerID, driveService, channelFileJob, &downloadWaitGroup, &statusTracker)
	}

	fmt.Println("Iniciando escaneamento e download simultaneamente...")
	discoveryWaitGroup.Add(1)
	go discoverAndQueueFiles(driveService, folderID, downloadPath, channelFileJob, &downloadWaitGroup, &discoveryWaitGroup, &statusTracker)

	discoveryWaitGroup.Wait()
	statusTracker.isDiscoveryFinished.Store(true)

	close(channelFileJob)

	downloadWaitGroup.Wait()

	channelIsDone <- true

	fmt.Println()
}

func printStatus(statusTracker *statusTracker, done chan bool) {
	for {
		select {
		case <-done:
			total := statusTracker.totalFilesFound.Load()
			skipped := statusTracker.skippedFiles.Load()
			finalLine := fmt.Sprintf("\rProgresso: %d / %d concluídos (Pulados: %d) - Finalizado!                \n", total, total, skipped)
			fmt.Print(finalLine)
			return
		default:
			completed := statusTracker.completedFiles.Load()
			totalFound := statusTracker.totalFilesFound.Load()
			skipped := statusTracker.skippedFiles.Load()

			percentage := float64(0)
			if totalFound > 0 {
				percentage = (float64(completed) / float64(totalFound)) * 100
			}

			etaStr := "--:--:--"
			elapsedSeconds := time.Since(statusTracker.startTime).Seconds()

			actualDownloads := completed - skipped

			if actualDownloads > 5 && elapsedSeconds > 3 {
				rate := float64(actualDownloads) / elapsedSeconds

				remainingFiles := float64(171000 - completed)

				if rate > 0 {
					etaSeconds := remainingFiles / rate
					h := int(etaSeconds) / 3600
					m := (int(etaSeconds) % 3600) / 60
					s := int(etaSeconds) % 60
					etaStr = fmt.Sprintf("%02d:%02d:%02d", h, m, s)
				}
			}

			discoveryStatus := ""
			if !statusTracker.isDiscoveryFinished.Load() {
				discoveryStatus = "(Escaneando...)"
			}

			statusLine := fmt.Sprintf("\rProgresso: %d/%d (%.2f%%) | Pulados: %d %s| ETA: %s  ", completed, totalFound, percentage, skipped, discoveryStatus, etaStr)
			fmt.Print(statusLine)

			time.Sleep(200 * time.Millisecond)
		}
	}
}

func startDownloadWorker(workerID int, driverService *drive.Service, channelFileJob <-chan *fileJob, waitGroup *sync.WaitGroup, statusTracker *statusTracker) {
	defer waitGroup.Done()
	for fileJob := range channelFileJob {
		if strings.HasPrefix(fileJob.file.MimeType, "application/vnd.google-apps") {
			convertGoogleFileType(driverService, fileJob.file, fileJob.localPath, statusTracker)
		} else {
			downloadFile(driverService, fileJob.file, fileJob.localPath, statusTracker)
		}
		statusTracker.completedFiles.Add(1)
	}
}

func downloadFile(srv *drive.Service, f *drive.File, filePath string, statusTracker *statusTracker) {
	if _, error := os.Stat(filePath); error == nil {
		statusTracker.skippedFiles.Add(1)
		return
	}

	log.Println(filePath)
	tempFilePath := filePath + ".tmp"
	resp, error := srv.Files.Get(f.Id).Download()
	if error != nil {
		log.Printf("download '%s': %v", f.Name, error)
		return
	}
	defer resp.Body.Close()

	out, error := os.Create(tempFilePath)
	if error != nil {
		log.Printf("create temp '%s': %v", tempFilePath, error)
		return
	}
	defer out.Close()

	_, error = io.Copy(out, resp.Body)
	if error != nil {
		out.Close()
		os.Remove(tempFilePath)
		log.Printf("copy '%s': %v", f.Name, error)
		return
	}

	if error := os.Rename(tempFilePath, filePath); error != nil {
		log.Printf("rename '%s': %v", filePath, error)
	}
}

func convertGoogleFileType(driveService *drive.Service, driveFile *drive.File, filePath string, statusTracker *statusTracker) {
	var exportMimeType, extension string
	switch driveFile.MimeType {
	case "application/vnd.google-apps.document":
		exportMimeType, extension = "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx"
	case "application/vnd.google-apps.spreadsheet":
		exportMimeType, extension = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx"
	case "application/vnd.google-apps.presentation":
		exportMimeType, extension = "application/vnd.openxmlformats-officedocument.presentationml.presentation", ".pptx"
	default:
		return
	}

	finalFilePath := filePath + extension
	if _, error := os.Stat(finalFilePath); error == nil {
		statusTracker.skippedFiles.Add(1)
		return
	}

	log.Println(filePath)
	tempFilePath := finalFilePath + ".tmp"
	response, error := driveService.Files.Export(driveFile.Id, exportMimeType).Download()
	if error != nil {
		errorLog.Printf("export '%s': %v", driveFile.Name, error)
		return
	}
	defer response.Body.Close()

	out, error := os.Create(tempFilePath)
	if error != nil {
		errorLog.Printf("create temp '%s': %v", tempFilePath, error)
		return
	}
	defer out.Close()

	_, error = io.Copy(out, response.Body)
	if error != nil {
		out.Close()
		os.Remove(tempFilePath)
		errorLog.Printf("copy response to file '%s': %v", driveFile.Name, error)
		return
	}

	if error := os.Rename(tempFilePath, finalFilePath); error != nil {
		errorLog.Printf("rename '%s': %v", finalFilePath, error)
	}
}

func discoverAndQueueFiles(driveService *drive.Service, folderID, localPath string, channelFileJob chan<- *fileJob, downloadWaitGroup, discoveryWaitGroup *sync.WaitGroup, statusTracker *statusTracker) {
	defer discoveryWaitGroup.Done()
	var discover func(string, string)
	discover = func(currentFolderId, currentLocalPath string) {
		if error := os.MkdirAll(currentLocalPath, 0755); error != nil {
			log.Printf("ao criar diretório local '%s': %v", currentLocalPath, error)
			return
		}
		var pageToken string
		for {
			query := fmt.Sprintf("'%s' in parents and trashed=false", currentFolderId)
			driveFileList, error := driveService.Files.List().Q(query).PageSize(1000).Fields("nextPageToken, files(id, name, mimeType)").PageToken(pageToken).Do()
			if error != nil {
				log.Printf("ao listar arquivos na pasta ID '%s': %v", currentFolderId, error)
				return
			}
			for _, file := range driveFileList.Files {
				sanitizedName := sanitizeFileName(file.Name)
				newLocalPath := filepath.Join(currentLocalPath, sanitizedName)
				if file.MimeType == "application/vnd.google-apps.folder" {
					discover(file.Id, newLocalPath)
				} else {
					statusTracker.totalFilesFound.Add(1)
					downloadWaitGroup.Add(1)
					channelFileJob <- &fileJob{file: file, localPath: newLocalPath}
				}
			}
			pageToken = driveFileList.NextPageToken
			if pageToken == "" {
				break
			}
		}
	}
	discover(folderID, localPath)
}

func authenticate(ctx context.Context) *drive.Service {
	b, error := ioutil.ReadFile("credentials.json")
	if error != nil {
		log.Fatalf("Não foi possível ler o arquivo de credenciais (credentials.json): %v", error)
	}
	config, error := google.ConfigFromJSON(b, drive.DriveReadonlyScope)
	if error != nil {
		log.Fatalf("Não foi possível processar o arquivo de credenciais: %v", error)
	}
	client := getClient(config)
	srv, error := drive.NewService(ctx, option.WithHTTPClient(client))
	if error != nil {
		log.Fatalf("Não foi possível criar o serviço do Drive: %v", error)
	}
	return srv
}

func getDriveFolderIDByPath(driveService *drive.Service, path string) (string, error) {
	if path == "" || path == "root" {
		return "root", nil
	}
	parts := strings.Split(path, "/")
	currentParentID := "root"
	for _, part := range parts {
		if part == "" {
			continue
		}
		query := fmt.Sprintf("mimeType='application/vnd.google-apps.folder' and name='%s' and '%s' in parents and trashed=false", part, currentParentID)
		r, error := driveService.Files.List().Q(query).Fields("files(id)").PageSize(1).Do()
		if error != nil {
			return "", fmt.Errorf("falha ao buscar pela pasta '%s': %v", part, error)
		}
		if len(r.Files) == 0 {
			return "", fmt.Errorf("a pasta '%s' não foi encontrada", part)
		}
		currentParentID = r.Files[0].Id
	}
	return currentParentID, nil
}

func sanitizeFileName(fileName string) string {
	invalidChars := []string{"\\", "/", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range invalidChars {
		fileName = strings.ReplaceAll(fileName, char, "_")
	}
	return fileName
}

func getClient(config *oauth2.Config) *http.Client {
	tokFile := "token.json"
	tok, error := tokenFromFile(tokFile)
	if error != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Acesse o seguinte link no seu navegador e cole o código de autorização aqui: \n%v\n", authURL)
	var authCode string
	if _, error := fmt.Scan(&authCode); error != nil {
		log.Fatalf("Não foi possível ler o código de autorização: %v", error)
	}
	tok, error := config.Exchange(context.TODO(), authCode)
	if error != nil {
		log.Fatalf("Não foi possível trocar o código pelo token: %v", error)
	}
	return tok
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, error := os.Open(file)
	if error != nil {
		return nil, error
	}
	defer f.Close()
	tok := &oauth2.Token{}
	error = json.NewDecoder(f).Decode(tok)
	return tok, error
}

func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Salvando o token de acesso em: %s\n", path)
	f, error := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if error != nil {
		log.Fatalf("Não foi possível salvar o token: %v", error)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}
