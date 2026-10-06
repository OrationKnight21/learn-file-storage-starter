package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse", err)
		return
	}
	v, o, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't retreive video file", err)
		return
	}
	defer v.Close()
	mediaType := o.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type", nil)
		return
	}
	vid, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "record not found", err)
		return
	}
	if vid.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "unauthorized user", nil)
		return
	}
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 || parts[1] == "" {
		respondWithError(w, http.StatusBadRequest, "malformed content-type", nil)
		return
	}
	extensionType := parts[1]
	fileName := fmt.Sprintf("%s.%s", videoIDString, extensionType)
	dataPath := filepath.Join(cfg.assetsRoot, fileName)
	file, err := os.Create(dataPath)
	if err != nil {
		respondWithError(w, http.StatusNotImplemented, "could not create the file", err)
		return
	}
	defer file.Close()
	_, err = io.Copy(file, v)
	if err != nil {
		respondWithError(w, http.StatusNotImplemented, "could not copy contents", err)
		return
	}
	thumnailUrl := fmt.Sprintf("http://localhost:%v/assets/%s.%s", cfg.port, vid.ID, extensionType)
	vid.ThumbnailURL = &thumnailUrl
	err = cfg.db.UpdateVideo(vid)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't update video", err)
		return
	}
	respondWithJSON(w, http.StatusOK, vid)
}
