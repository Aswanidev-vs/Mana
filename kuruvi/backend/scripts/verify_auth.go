package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	// Added a timeout to the client to prevent hanging indefinitely
	client := &http.Client{Timeout: 10 * time.Second}
	baseURL := "http://localhost:8080/api"

	// 1. Test Registration
	user := fmt.Sprintf("testuser-%d", time.Now().Unix())
	regData := map[string]string{"username": user, "password": "password123"}
	body, _ := json.Marshal(regData)

	fmt.Printf("Testing registration for %s...\n", user)
	resp, err := client.Post(baseURL+"/auth/register", "application/json", bytes.NewBuffer(body))
	if err != nil {
		fmt.Printf("Registration failed: %v\n", err)
		os.Exit(1)
	}
	
	// Read and close the body immediately to avoid resource leaks when reusing 'resp'
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		fmt.Printf("Unexpected registration status: %d\n", resp.StatusCode)
		os.Exit(1)
	}
	fmt.Println("Registration successful!")

	// 2. Test Login
	fmt.Printf("Testing login for %s...\n", user)
	resp, err = client.Post(baseURL+"/auth/login", "application/json", bytes.NewBuffer(body))
	if err != nil {
		fmt.Printf("Login failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Unexpected login status: %d\n", resp.StatusCode)
		os.Exit(1)
	}

	var loginResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		fmt.Printf("Failed to decode response: %v\n", err)
		os.Exit(1)
	}

	if loginResp["token"] == "" {
		fmt.Println("Login failed: empty token in response")
		os.Exit(1)
	}
	fmt.Println("Login successful! Token received.")
}
