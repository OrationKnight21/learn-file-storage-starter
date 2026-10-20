package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading video", videoID, "by user", userID)

	const maxUploadLimit = 1 << 30
	v, o, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't retreive video file", err)
		return
	}
	defer v.Close()
	p := http.MaxBytesReader(w, v, maxUploadLimit)
	vid, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "record not found", err)
		return
	}
	if vid.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "unauthorized user", nil)
		return
	}
	mediaType, _, err := mime.ParseMediaType(o.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "content-type missing", err)
		return
	}
	if "video/mp4" != mediaType {
		respondWithError(w, http.StatusBadRequest, "wrong content-type", nil)
		return
	}
	fileName := "tubely-upload.mp4"
	file, err := os.CreateTemp("", fileName)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not create file", err)
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()
	_, err = io.Copy(file, p)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not copy contents", err)
		return
	}
	aspectRatio, err := getVideoAspectRatio(file.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "could not get the aspect ratio", err)
		return
	}
	var prefix string
	switch aspectRatio {
	case "9:16":
		prefix = "portrait"
	case "16:9":
		prefix = "landscape"
	default:
		prefix = "other"
	}
	processedFilePath, err := processVideoForFastStart(file.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not process the file", err)
		return
	}
	processedFile, err := os.Open(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "could not open the file/ file not found", err)
		return
	}
	defer os.Remove(processedFile.Name())
	defer processedFile.Close()
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "could not set the offset", err)
		return
	}
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 || parts[1] == "" {
		respondWithError(w, http.StatusBadRequest, "malformed content-type", nil)
		return
	}
	extensionType := parts[1]
	randSlice := make([]byte, 32)
	_, err = rand.Read(randSlice)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error reading", err)
		return
	}
	encoded := base64.RawURLEncoding.EncodeToString(randSlice)
	fileKey := fmt.Sprintf("%s/%s.%s", prefix, encoded, extensionType)
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(fileKey),
		Body:        processedFile,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't put the object", err)
		return
	}
	videoUrl := fmt.Sprintf("%s,%s", cfg.s3Bucket, fileKey)
	vid.VideoURL = &videoUrl
	err = cfg.db.UpdateVideo(vid)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't update video", err)
		return
	}
	signedVideo, err := cfg.dbVideoToSignedVideo(vid)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not sign the video", err)
		return
	}
	respondWithJSON(w, http.StatusOK, signedVideo)
}
func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	client := s3.NewPresignClient(s3Client)
	presignedUrl, err := client.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}
	return presignedUrl.URL, nil
}
func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	s := strings.Split(*video.VideoURL, ",")
	if len(s) < 2 {
		return database.Video{}, errors.New("insufficient values")
	}
	bucket := s[0]
	key := s[1]
	presignedUrl, err := generatePresignedURL(cfg.s3Client, bucket, key, 5*time.Minute)
	if err != nil {
		return database.Video{}, err
	}
	video.VideoURL = &presignedUrl
	return video, nil
}
