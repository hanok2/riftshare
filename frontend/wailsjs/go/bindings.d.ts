interface go {
  "main": {
    "App": {
		CancelWormholeRequest():Promise<void>
		GetCurrentVersion():Promise<string>
		GetDownloadsFolder():Promise<string>
		GetNotificationsParam():Promise<boolean>
		GetOverwriteParam():Promise<boolean>
		OpenDirectoryDialog():Promise<Array<string>>
		OpenFile(arg1:string):Promise<void>
		OpenFilesDialog():Promise<Array<string>>
		ReceiveFile(arg1:string):Promise<void>
		SelectedFilesSend():Promise<void>
		SendDirectory(arg1:string):Promise<void>
		SendFile(arg1:string):Promise<void>
		SetDownloadsFolder():Promise<string>
		SetNotificationsParam(arg1:boolean):Promise<boolean>
		SetOverwriteParam(arg1:boolean):Promise<boolean>
		ShowErrorDialog(arg1:string):Promise<void>
		UpdateCheckUI():Promise<void>
		UpdateSendProgress(arg1:number,arg2:number):Promise<void>
    },
  }

}

declare global {
	interface Window {
		go: go;
	}
}
