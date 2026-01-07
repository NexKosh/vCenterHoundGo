package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	serverPtr := flag.String("s", "", "BloodHound URL (e.g. http://localhost:8080)")
	userPtr := flag.String("u", "", "Username")
	passPtr := flag.String("p", "", "Password")
	modelPtr := flag.String("model", "model.json", "Path to model.json file")

	// Support long flags too
	flag.StringVar(serverPtr, "server", "", "BloodHound URL")
	flag.StringVar(userPtr, "username", "", "Username")
	flag.StringVar(passPtr, "password", "", "Password")

	flag.Parse()

	if *serverPtr == "" || *userPtr == "" || *passPtr == "" {
		flag.Usage()
		fmt.Println("\nExample:\n  vCenterSchemaUploader.exe -s http://localhost:8080 -u admin -p password")
		os.Exit(1)
	}

	log.Println("Starting Schema Upload...")
	err := UploadSchema(*serverPtr, *userPtr, *passPtr, *modelPtr)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	log.Println("Done.")
}

// UploadSchema authenticates and uploads the model file to BloodHound
func UploadSchema(baseURL, username, password, modelPath string) error {
	// Ensure URL has protocol
	if !strings.HasPrefix(baseURL, "http") {
		baseURL = "http://" + baseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 1. Login
	log.Printf("Connecting to %s...", baseURL)
	token, err := login(client, baseURL, username, password)
	if err != nil {
		return fmt.Errorf("login failed: %v", err)
	}
	log.Println("Successfully authenticated with BloodHound")

	// 2. Read Model
	log.Printf("Reading model file: %s", modelPath)
	modelData, err := ioutil.ReadFile(modelPath)
	if err != nil {
		return fmt.Errorf("failed to read model file: %v", err)
	}

	// 3. Upload
	log.Println("Uploading custom nodes schema...")
	err = upload(client, baseURL, token, modelData)
	if err != nil {
		return fmt.Errorf("upload failed: %v", err)
	}

	log.Println("Model uploaded successfully!")
	return nil
}

func login(client *http.Client, baseURL, username, password string) (string, error) {
	loginURL := baseURL + "/api/v2/login"

	reqBody := map[string]string{
		"login_method": "secret",
		"username":     username,
		"secret":       password,
	}

	jsonBody, _ := json.Marshal(reqBody)

	resp, err := client.Post(loginURL, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			SessionToken string `json:"session_token"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Data.SessionToken == "" {
		return "", fmt.Errorf("no session token in response")
	}

	return result.Data.SessionToken, nil
}

func upload(client *http.Client, baseURL, token string, data []byte) error {
	uploadURL := baseURL + "/api/v2/custom-nodes"

	req, err := http.NewRequest("POST", uploadURL, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
