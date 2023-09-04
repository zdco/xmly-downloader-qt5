package main

import "C"
import (
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unsafe"

	"github.com/cavaliercoder/grab"
	xmly "github.com/zdco/xmlydownloader"
)

//#include "cgo.h"
import "C"

func main() {
	log.SetFlags(log.Lshortfile | log.Ltime)
}

var uflCallback C.UpdateFileLengthCallback

//export CgoRegisterCallback
func CgoRegisterCallback(callback C.UpdateFileLengthCallback) {
	uflCallback = callback
}

//export CgoGetAlbumInfo
func CgoGetAlbumInfo(albumID C.int) *C.DataError {
	ai, err := xmly.GetAlbumInfo(int(albumID))
	if err != nil {
		return C.newDataError(nil, C.CString(err.Error()))
	}

	var freeTrackIDs *C.char = nil
	if len(ai.Data.Album.PriceTypes) > 0 {
		freeTrackIDs = C.CString(ai.Data.Album.PriceTypes[0].FreeTrackIds)
	}
	pAlbumInfo := C.newAlbumInfo(C.CString(ai.Data.Album.Title), C.int(ai.Data.Album.TrackCount),
		C.int(ai.AlbumType()), freeTrackIDs)
	return C.newData(unsafe.Pointer(pAlbumInfo))
}

//export CgoGetTrackList
func CgoGetTrackList(albumID, pageID, isAsc C.int) *C.DataError {
	b := false
	if int(isAsc) != 0 {
		b = true
	}
	tracks, err := xmly.GetTrackList(int(albumID), int(pageID), b)
	if err != nil {
		return C.newDataError(nil, C.CString(err.Error()))
	}

	ptrArray := C.newPointerArray(C.int(len(tracks.Data.List)))
	for i, v := range tracks.Data.List {
		v.Title = formatFileName(v.Title)
		C.setPointerArray(ptrArray, C.int(i), C.newTrackInfo(C.int(v.TrackID), C.CString(v.Title), C.int(v.Duration),
			C.CString(v.PlayURL32), C.CString(v.PlayURL64), C.CString(v.PlayPathAacv224), C.CString(v.PlayPathAacv164)))
	}

	cArray := C.newCArray(ptrArray, C.int(len(tracks.Data.List)))
	cPlaylist := C.newTrackList(C.int(tracks.Data.MaxPageID), unsafe.Pointer(cArray))
	return C.newData(unsafe.Pointer(cPlaylist))
}

//export CgoGetChargeTrackInfo
func CgoGetChargeTrackInfo(trackID C.int, cookie *C.char) *C.DataError {
	ai, err := xmly.GetVipAudioInfo(int(trackID), C.GoString(cookie))
	if err != nil {
		return C.newDataError(nil, C.CString(err.Error()))
	}
	ai.Title = formatFileName(ai.Title)
	return C.newData(C.newTrackInfo(C.int(ai.TrackID), C.CString(ai.Title), C.int(ai.Duration), nil, nil, nil,
		C.CString(ai.PlayPathAacv164)))
}

var grabClient = grab.Client{
	UserAgent: xmly.UserAgentPC,
	HTTPClient: &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	},
}

//export CgoDownloadFile
func CgoDownloadFile(cUrl, cFilePath *C.char, id C.int) *C.char {
	url := C.GoString(cUrl)
	filePath := C.GoString(cFilePath)

	req, err := grab.NewRequest(filePath, url)
	if err != nil {
		return C.CString(err.Error())
	}
	resp := grabClient.Do(req)
	contentLength := C.long(resp.Size)
	currentLength := C.long(0)
	C.UpdateFileLength(uflCallback, id, &contentLength, &currentLength)
	//判断同名文件是否存在并对比大小
	if fileInfo, err := os.Stat(filePath); err == nil {
		if fileInfo.Size() == resp.Size {
			err = resp.Cancel()
			if err != nil {
				return C.CString(err.Error())
			}
			return nil
		}
	}

	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	var lastProgress float64
	count := 0
	for {
		select {
		case <-t.C:
			//超时(2s)
			if count == 20 {
				err = resp.Cancel()
				if err != nil {
					return C.CString("无法下载文件: 已超时: " + err.Error())
				}
				return C.CString("无法下载文件: 已超时")
			}
			if lastProgress == resp.Progress() {
				count++
			} else {
				lastProgress = resp.Progress()
			}

			currentLength := C.long(resp.BytesComplete())
			C.UpdateFileLength(uflCallback, id, &contentLength, &currentLength)
		case <-resp.Done:
			if err := resp.Err(); err != nil {
				return C.CString("无法下载文件: " + err.Error())
			}
			return nil
		}
	}
}

//export CgoGetUserInfo
func CgoGetUserInfo(cookie *C.char) *C.DataError {
	ui, err := xmly.GetUserInfo(C.GoString(cookie))
	if err != nil {
		return C.newDataError(nil, C.CString(err.Error()))
	}

	isVip := 0
	if ui.Data.IsVip {
		isVip = 1
	}
	var p unsafe.Pointer
	p = C.newUserInfo(C.int(ui.Ret), C.CString(ui.Msg), C.int(ui.Data.UID), C.int(isVip), C.CString(ui.Data.Nickname))
	return C.newData(p)
}

//export CgoGetQRCode
func CgoGetQRCode() *C.DataError {
	qrCode, err := xmly.GetQRCode()
	if err != nil {
		return C.newDataError(nil, C.CString(err.Error()))
	}

	p := C.newQRCode(C.int(qrCode.Ret), C.CString(qrCode.Msg), C.CString(qrCode.QrID), C.CString(qrCode.Img))
	return C.newData(p)
}

//export CgoCheckQRCode
func CgoCheckQRCode(qrID *C.char) *C.char {
	status, cookie, err := xmly.CheckQRCodeStatus(C.GoString(qrID))
	if err != nil {
		return nil
	}
	if status.Ret == 0 {
		return C.CString(cookie)
	}
	return nil
}

var fileNameRegexp = regexp.MustCompile("[/\\:*?\"<>|]")

func formatFileName(s string) string {
	s = fileNameRegexp.ReplaceAllLiteralString(s, " ")
	s = strings.ReplaceAll(s, "\t", "")
	s = strings.TrimSpace(s)
	return s
}
