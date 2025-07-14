package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// WebSocket bağlantısını HTTP üzerinden yükseltmek için kullanılır
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Tüm istemcileri ve kullanıcı adlarını eşle
var clients = make(map[*websocket.Conn]string)
var clientsMutex = sync.Mutex{}

// Gelen mesajlar burada toplanır
var broadcast = make(chan string)

func main() {
	http.HandleFunc("/ws", handleConnections)
	http.Handle("/", http.FileServer(http.Dir("./")))

	go handleMessages()

	fmt.Println("WebSocket sunucusu http://localhost:8080/ws adresinde çalışıyor...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Yükseltme hatası:", err)
		return
	}
	defer ws.Close()

	var username string

	// İlk gelen mesaj kullanıcı adıdır
	_, msg, err := ws.ReadMessage()
	if err != nil {
		log.Println("Kullanıcı adı alınamadı:", err)
		return
	}
	username = string(msg)

	clientsMutex.Lock()
	clients[ws] = username
	clientsMutex.Unlock()

	// Katılım mesajı
	broadcast <- fmt.Sprintf("🔵 %s katıldı", username)

	// Aktif kullanıcı listesini gönder
	sendActiveUsers()

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Printf("Kullanıcı ayrıldı: %s\n", username)

			clientsMutex.Lock()
			delete(clients, ws)
			clientsMutex.Unlock()

			broadcast <- fmt.Sprintf("🔴 %s ayrıldı", username)
			sendActiveUsers()
			break
		}

		text := emojiParser(string(msg))
		broadcast <- fmt.Sprintf("%s: %s", username, text)
	}
}

func handleMessages() {
	for {
		msg := <-broadcast
		clientsMutex.Lock()
		for client := range clients {
			err := client.WriteMessage(websocket.TextMessage, []byte(msg))
			if err != nil {
				log.Println("Mesaj gönderme hatası:", err)
				client.Close()
				delete(clients, client)
			}
		}
		clientsMutex.Unlock()
	}
}

// Aktif kullanıcı listesini tüm istemcilere gönder
func sendActiveUsers() {
	userList := "👥 Aktif kullanıcılar: "
	clientsMutex.Lock()
	users := []string{}
	for _, name := range clients {
		users = append(users, name)
	}
	clientsMutex.Unlock()
	userList += strings.Join(users, ", ")
	broadcast <- userList
}

// Basit emoji dönüştürücü
func emojiParser(text string) string {
	replacements := map[string]string{
		":smile:": "😄",
		":heart:": "❤️",
		":fire:":  "🔥",
		":thumbs:": "👍",
		":ok:":    "👌",
	}
	for key, val := range replacements {
		text = strings.ReplaceAll(text, key, val)
	}
	return text
}
