package main

import (
	"encoding/json" 
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
)

// WebSocket bağlantısını HTTP üzerinden yükseltmek için 
// Gorilla WebSocket kütüphanesinden Upgrader yapısı kullanılır		 
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// clients haritası, WebSocket bağlantılarını ve kullanıcı adlarını tutar
// clientsMutex, eşzamanlı erişim için mutex kullanılır	
var clients = make(map[*websocket.Conn]string)
var clientsMutex = sync.Mutex{}

// Gelen mesajlar burada toplanır
// broadcast kanalı, tüm istemcilere mesaj göndermek için kullanılır
var broadcast = make(chan string)

// db değişkeni, SQLite veritabanı bağlantısını temsil eder
var db *sql.DB
 
// main fonksiyonu, WebSocket sunucusunu başlatır
// HTTP sunucusu "/ws" yolunda WebSocket bağlantılarını dinler
// Ayrıca, kök dizinde statik dosyaları sunar
// handleConnections fonksiyonu, yeni WebSocket bağlantılarını işler
// handleMessages fonksiyonu, gelen mesajları alır ve tüm istemcilere iletirir
// sendActiveUsers fonksiyonu, aktif kullanıcı listesini tüm istemcilere gönderir
// emojiParser fonksiyonu, metindeki basit emojileri dönüştürür
// Program, "WebSocket sunucusu http://localhost:8080/ws adresinde çalışıyor..." mesajını yazdırır
// HTTP sunucusunu dinlemeye başlar
func main() {
	var err error
	// SQLite veritabanı bağlantısı açılır (chat.db dosyası)
	db, err = sql.Open("sqlite3", "./chat.db")
	if err != nil {
		log.Fatal("Veritabanı açma hatası:", err)
	}
	defer db.Close()

	// Mesajlar tablosu yoksa oluşturulur
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT,
		message TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		log.Fatal("Tablo oluşturma hatası:", err)
	}

	// WebSocket bağlantılarını dinler
	http.HandleFunc("/ws", handleConnections)
	http.Handle("/", http.FileServer(http.Dir("./")))
	// Gelen mesajları işlemek için ayrı bir goroutine başlatır
	go handleMessages()
	// Sunucu başlatılır ve 8080 portunda dinlenir
	fmt.Println("WebSocket sunucusu http://localhost:8080/ws adresinde çalışıyor...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// handleConnections fonksiyonu, yeni WebSocket bağlantılarını işler
// İlk gelen mesaj kullanıcı adıdır ve bu kullanıcı adı ile bağlantı kaydedilir
// Kullanıcı katıldığında ve ayrıldığında mesajlar yayınlanır
// Aktif kullanıcı listesi güncellenir ve tüm istemcilere gönderilir
// Kullanıcıdan gelen mesajlar okunur ve emoji dönüştürücü ile işlenir
// Herhangi bir hata durumunda, bağlantı kapatılır ve kullanıcı listesi güncellenir
func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Yükseltme hatası:", err)
		return
	}
	// Bağlantı kapatıldığında kaynakları temizlemek için defer kullanılır
	defer ws.Close()
	// Yeni bir kullanıcı adı değişkeni tanımlanır
	var username string

	// İlk gelen mesaj kullanıcı adıdır
	_, msg, err := ws.ReadMessage()
	if err != nil {
		log.Println("Kullanıcı adı alınamadı:", err)
		return
	}
	username = string(msg)
	log.Printf("Yeni kullanıcı: %s\n", username)
	if strings.TrimSpace(username) == "" {
		log.Println("Kullanıcı adı boş olamaz")
		return
	}

	clientsMutex.Lock()
	for _, existingUsername := range clients {
		if existingUsername == username {
			log.Printf("Kullanıcı adı zaten kullanılıyor: %s\n", username)
			ws.WriteMessage(websocket.TextMessage, []byte("Kullanıcı adı zaten kullanılıyor"))
			ws.Close()
			clientsMutex.Unlock()
			return
		}
	}
	clients[ws] = username
	clientsMutex.Unlock()

	// Katılım mesajı
	broadcast <- fmt.Sprintf("🔵 %s katıldı", username)

	// ✅ Yeni bağlanan kullanıcıya son 10 mesajı veritabanından gönder
	sendLastMessages(ws)

	// Aktif kullanıcı listesini gönder
	sendActiveUsers()

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Printf("Kullanıcı ayrıldı: %s\n", username)
			log.Println("Hata:", err)

			clientsMutex.Lock()
			delete(clients, ws)
			clientsMutex.Unlock()

			broadcast <- fmt.Sprintf("🔴 %s ayrıldı", username)
			sendActiveUsers()
			break
		}
		text := emojiParser(string(msg))
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		broadcast <- fmt.Sprintf("%s: %s [%s]", username, text, timestamp)
	}
}

func handleMessages() {
	for {
		msg := <-broadcast
		saveMessageToDB(msg)

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

// ✅ Aktif kullanıcı listesini JSON formatında tüm istemcilere gönderir
func sendActiveUsers() {
	users := []string{}
	clientsMutex.Lock()
	for _, name := range clients {
		users = append(users, name)
	}
	clientsMutex.Unlock()

	data := map[string]interface{}{
		"type":  "active_users",
		"users": users,
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Println("JSON formatlama hatası:", err)
		return
	}

	clientsMutex.Lock()
	for client := range clients {
		err := client.WriteMessage(websocket.TextMessage, jsonData)
		if err != nil {
			log.Println("Aktif kullanıcı listesi gönderme hatası:", err)
			client.Close()
			delete(clients, client)
		}
	}
	clientsMutex.Unlock()
}

// Basit emoji dönüştürücü
func emojiParser(text string) string {
	replacements := map[string]string{
		":smile:": "😄",
		":heart:": "❤️",
		":fire:":  "🔥",
		":thumbs:": "👍",
		":ok:":     "👌",
	}
	for key, val := range replacements {
		text = strings.ReplaceAll(text, key, val)
	}
	return text
}

// saveMessageToDB fonksiyonu, gelen mesajı SQLite veritabanına kaydeder
func saveMessageToDB(msg string) {
	username := "Sistem"
	message := msg

	if strings.Contains(msg, ": ") {
		parts := strings.SplitN(msg, ": ", 2)
		username = parts[0]
		message = parts[1]
	}

	_, err := db.Exec("INSERT INTO messages(username, message) VALUES(?, ?)", username, message)
	if err != nil {
		log.Println("Mesaj veritabanına kaydedilemedi:", err)
	}
}

func sendLastMessages(ws *websocket.Conn) {
	rows, err := db.Query(`SELECT username, message, timestamp FROM messages ORDER BY id DESC LIMIT 10`)
	if err != nil {
		log.Println("Geçmiş mesajları çekerken hata:", err)
		return
	}
	defer rows.Close()

	var messages []string
	for rows.Next() {
		var username, message, timestamp string
		rows.Scan(&username, &message, &timestamp)
		messages = append([]string{fmt.Sprintf("%s: %s [%s]", username, message, timestamp)}, messages...)
	}

	for _, msg := range messages {
		err := ws.WriteMessage(websocket.TextMessage, []byte(msg))
		if err != nil {
			log.Println("Geçmiş mesaj gönderme hatası:", err)
			return
		}
	}
}
