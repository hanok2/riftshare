package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"time"

	"riftshare/transport"

	"github.com/psanford/wormhole-william/wormhole"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/klauspost/compress/zip"
)

// App application struct
type App struct {
	ctx            context.Context
	c              *transport.Client
	selectedFiles  []string
	wormholeCtx    *context.Context
	wormholeCancel *context.CancelFunc
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{c: transport.NewClient()}
}

// startup is called at application startup
func (b *App) startup(ctx context.Context) {
	// Perform your setup here
	b.ctx = ctx
	settings, err := GetUserSettings()
	if err != nil {
		log.Println(err)
	}
	b.c.Notifications = settings.Notifications
	b.c.OverwriteExisting = settings.Overwrite
	b.c.DownloadPath = settings.DownloadsDirectory
}

// domReady is called after the front-end dom has been loaded
func (b *App) domReady(ctx context.Context) {
	// Add your action here
	b.UpdateCheckUI()
	// buttons := []string{"Yes", "No"}
	// dialogOpts := runtime.MessageDialogOptions{Title: "Update Available", Message: "Test", Type: runtime.QuestionDialog, Buttons: buttons, DefaultButton: "Yes", CancelButton: "No"}
	// _, err := runtime.MessageDialog(b.ctx, dialogOpts)
	// if err != nil {
	// 	runtime.LogError(b.ctx, "Error in update dialog. ")
	// }

	// err := beeep.Notify("RiftShare", "Send Complete", "./appicon.png")
	// if err != nil {
	// 	panic(err)
	// }
}

// shutdown is called at application termination
func (b *App) shutdown(ctx context.Context) {
	// Perform your teardown here
	settings := UserSettings{
		Notifications:      b.c.Notifications,
		Overwrite:          b.c.OverwriteExisting,
		DownloadsDirectory: b.c.DownloadPath,
	}
	err := SaveUserSettings(settings)
	if err != nil {
		log.Println("Could not persist user settings")
	}
}

// Greet returns a greeting for the given name
func (b *App) OpenDirectoryDialog() []string {
	opts := runtime.OpenDialogOptions{Title: "Select Directory", DefaultDirectory: b.GetDownloadsFolder(), AllowDirectories: true}
	selection, err := runtime.OpenDirectoryDialog(b.ctx, opts)
	if err != nil {
		runtime.LogInfo(b.ctx, "Error opening dialog")
		b.ShowErrorDialog(err.Error())
	}
	runtime.LogInfo(b.ctx, "File Selected:"+selection)
	b.selectedFiles = []string{selection}
	return b.selectedFiles
}

func (b *App) OpenFilesDialog() []string {
	opts := runtime.OpenDialogOptions{Title: "Select File", AllowFiles: true, DefaultDirectory: b.GetDownloadsFolder()}
	selection, err := runtime.OpenMultipleFilesDialog(b.ctx, opts)
	if err != nil {
		runtime.LogInfo(b.ctx, "Error opening dialog")
	}
	runtime.LogInfo(b.ctx, "File Selected:")
	log.Println(selection)
	b.selectedFiles = selection
	return b.selectedFiles
}

func (b *App) SendFile(filePath string) {
	runtime.LogInfo(b.ctx, "Sending File: "+filePath)
	runtime.EventsEmit(b.ctx, "send:status", "retrieving code")

	go func() {
		ctx := *b.wormholeCtx
		code, status, err := b.c.NewFileSend(ctx, filePath, wormhole.WithProgress(b.UpdateSendProgress))
		if err != nil {
			runtime.LogError(b.ctx, "Send Failed")
			runtime.EventsEmit(b.ctx, "send:status", "failed")
			b.ShowErrorDialog(err.Error())
		}
		runtime.EventsEmit(b.ctx, "send:started", code)
		runtime.EventsEmit(b.ctx, "send:status", "waiting for receiver")

		select {
		case s := <-status:
			if s.Error != nil {
				runtime.LogError(b.ctx, "Send Failed")
				runtime.EventsEmit(b.ctx, "send:status", "failed")
				b.ShowErrorDialog(s.Error.Error())
			} else if s.OK {
				runtime.LogInfo(b.ctx, "Send Success")
				runtime.EventsEmit(b.ctx, "send:status", "completed")
			}

			if filepath.Ext(filePath) == ".zip" {
				os.Remove(filePath)
			}
		case <-ctx.Done():
			// If the request gets cancelled, log it
			// to STDERR
			log.Println("Request cancelled, removing zip file")
			if filepath.Ext(filePath) == ".zip" {
				os.Remove(filePath)
			}
		}
	}()
}

func (b *App) SendDirectory(dirPath string) {
	runtime.LogInfo(b.ctx, "Sending Directory: "+dirPath)
	runtime.EventsEmit(b.ctx, "send:status", "sending")

	go func() {
		code, status, err := b.c.NewDirSend(*b.wormholeCtx, dirPath, wormhole.WithProgress(b.UpdateSendProgress))
		if err != nil {
			runtime.LogError(b.ctx, "Send Failed")
			runtime.EventsEmit(b.ctx, "send:status", "failed")
			b.ShowErrorDialog(err.Error())
		}
		runtime.EventsEmit(b.ctx, "send:started", code)

		s := <-status

		if s.Error != nil {
			runtime.LogError(b.ctx, "Send Failed")
			runtime.EventsEmit(b.ctx, "send:status", "failed")
			b.ShowErrorDialog(s.Error.Error())
		} else if s.OK {
			runtime.LogInfo(b.ctx, "Send Success")
			runtime.EventsEmit(b.ctx, "send:status", "completed")
		}
	}()
}

func (b *App) ReceiveFile(code string) {
	runtime.LogInfo(b.ctx, "Receiving File...")
	runtime.EventsEmit(b.ctx, "receive:status", "receiving")

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	b.wormholeCtx = &ctx
	b.wormholeCancel = &cancel

	pathname := make(chan string, 1)
	progress := make(chan float64)
	runtime.EventsEmit(b.ctx, "receive:started")

	go func() {
		path := <-pathname
		runtime.LogInfo(b.ctx, path)
		runtime.EventsEmit(b.ctx, "receive:path", path)
		runtime.EventsEmit(b.ctx, "receive:status", "receiving")
	}()

	go func() {
		for percent := range progress {
			// log.Println(percent)
			runtime.EventsEmit(b.ctx, "receive:updated", percent)
		}
	}()

	go func() {
		err := b.c.NewReceive(*b.wormholeCtx, code, pathname, progress)
		if err != nil {
			runtime.LogError(b.ctx, "Receive Failed")
			runtime.EventsEmit(b.ctx, "receive:status", "failed")
			b.ShowErrorDialog(err.Error())
		}
		runtime.EventsEmit(b.ctx, "receive:status", "completed")
	}()
}

func (b *App) UpdateSendProgress(sentBytes int64, totalBytes int64) {
	percentage := math.Round(float64(sentBytes) / float64(totalBytes) * 100)
	// runtime.LogInfo(b.ctx, "Progress"+strconv.Itoa(int(percentage)))
	runtime.EventsEmit(b.ctx, "send:updated", percentage)
}

func (b *App) OpenFile(path string) {

	var err error
	switch goruntime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", path).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	case "darwin":
		err = exec.Command("open", path).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		log.Println(err)
	}

}

func (b *App) SelectedFilesSend() {
	// Create a new context
	ctx := context.Background()
	// Create a new context, with its cancellation function
	// from the original context
	ctx, cancel := context.WithCancel(ctx)
	b.wormholeCtx = &ctx
	b.wormholeCancel = &cancel
	if len(b.selectedFiles) == 1 {
		fileInfo, err := os.Stat(b.selectedFiles[0])
		if err != nil {
			runtime.LogError(b.ctx, "Could not check send file info")
		}
		if fileInfo.IsDir() {
			b.SendDirectory(b.selectedFiles[0])
		} else {
			b.SendFile(b.selectedFiles[0])
		}
	} else {
		archivePath := b.zipFiles(b.selectedFiles)
		b.SendFile(archivePath)
	}
}

func (b *App) zipFiles(pathNames []string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		runtime.LogError(b.ctx, "Could not find home directory")
	}
	runtime.LogInfo(b.ctx, "creating zip archive...")
	runtime.EventsEmit(b.ctx, "send:status", "compressing Files")
	timeString := time.Now().Format("2006-01-02-15-04-05")
	archiveName := fmt.Sprintf("wormhole-%s.zip", timeString)
	archivePath := filepath.Join(homeDir, archiveName)
	archive, err := os.Create(archivePath)
	if err != nil {
		runtime.LogFatal(b.ctx, "Error creating archive")
	}
	defer archive.Close()

	w := zip.NewWriter(archive)
	defer w.Close()

	for _, filePath := range pathNames {
		file, err := os.Open(filePath)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		f, err := w.Create(filepath.Base(file.Name()))
		if err != nil {
			log.Fatal(err)
		}
		_, err = io.Copy(f, file)
		if err != nil {
			log.Fatal(err)
		}
	}
	runtime.EventsEmit(b.ctx, "send:status", "zip Complete")

	return archivePath
}

func (b *App) CancelWormholeRequest() {
	runtime.LogInfo(b.ctx, "Cancelled wormhole request. ")
	cancel := *b.wormholeCancel
	cancel()
}

func (b *App) UpdateCheckUI() {
	shouldUpdate, latestVersion := checkForUpdate()
	if shouldUpdate {
		updateMessage := fmt.Sprintf("New Version Available, would you like to update to v%s", latestVersion)
		buttons := []string{"Yes", "No"}
		dialogOpts := runtime.MessageDialogOptions{Title: "Update Available", Message: updateMessage, Type: runtime.QuestionDialog, Buttons: buttons, DefaultButton: "Yes", CancelButton: "No"}
		action, err := runtime.MessageDialog(b.ctx, dialogOpts)
		if err != nil {
			runtime.LogError(b.ctx, "Error in update dialog. ")
		}
		runtime.LogInfo(b.ctx, action)
		if action == "Yes" {
			log.Println("Update clicked")
			updated := doSelfUpdate()
			if updated {
				buttons = []string{"Ok"}
				dialogOpts = runtime.MessageDialogOptions{Title: "Update Succeeded", Message: "Update Successfull. Please restart. ", Type: runtime.InfoDialog, Buttons: buttons, DefaultButton: "Ok"}
				runtime.MessageDialog(b.ctx, dialogOpts)
			} else {
				buttons = []string{"Ok"}
				dialogOpts = runtime.MessageDialogOptions{Title: "Update Error", Message: "Update failed, try again later. ", Type: runtime.InfoDialog, Buttons: buttons, DefaultButton: "Ok"}
				runtime.MessageDialog(b.ctx, dialogOpts)
			}
		}
	}
}

func (b *App) GetDownloadsFolder() string {
	return b.c.DownloadPath
}

func (b *App) GetCurrentVersion() string {
	return version
}

func (b *App) SetDownloadsFolder() string {
	opts := runtime.OpenDialogOptions{Title: "Select Directory", DefaultDirectory: b.GetDownloadsFolder(), AllowDirectories: true}
	selection, err := runtime.OpenDirectoryDialog(b.ctx, opts)
	if err != nil {
		runtime.LogInfo(b.ctx, "Error opening dialog")
		b.ShowErrorDialog(err.Error())
	}
	b.c.DownloadPath = selection
	return b.c.DownloadPath
}

func (b *App) SetOverwriteParam(val bool) bool {
	b.c.OverwriteExisting = val
	return b.c.OverwriteExisting
}

func (b *App) GetOverwriteParam() bool {
	return b.c.OverwriteExisting
}

func (b *App) SetNotificationsParam(val bool) bool {
	b.c.Notifications = val
	return b.c.Notifications
}

func (b *App) GetNotificationsParam() bool {
	return b.c.Notifications
}

func (b *App) ShowErrorDialog(message string) {
	buttons := []string{"Ok"}
	opts := runtime.MessageDialogOptions{Title: "Error Occured", Message: message, Buttons: buttons, Type: runtime.ErrorDialog, DefaultButton: "Ok"}
	runtime.MessageDialog(b.ctx, opts)
}
