package main

import (
	"database/sql"               // SQLite veritabanı için paket
	"encoding/json"              // JSON encode/decode işlemleri için
	"fmt"                        // Formatlı yazdırma için
	"log"                        // Loglama işlemleri için
	"net/http"                   // HTTP server için
	"strings"                    // String işlemleri için
	"sync"                       // Eşzamanlılık için mutex
	"time"                       // Zaman işlemleri için

	"github.com/golang-jwt/jwt/v5"         // JWT token üretmek ve doğrulamak için
	"github.com/gorilla/websocket"         // WebSocket bağlantıları için
	"golang.org/x/crypto/bcrypt"           // Şifreyi hashlemek için
	_ "github.com/mattn/go-sqlite3"        // SQLite sürücüsü (import edilmese çalışmaz)
)

// WebSocket bağlantılarını HTTP'den yükseltmek için upgrader kullanılır
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // Her kaynağa izin ver
}
// WebSocket bağlantıları için global değişkenler
// Bu değişkenler tüm bağlantıları ve kullanıcı adlarını tutar	
// Tüm aktif bağlantılar burada tutulur (connection -> username)
var clients = make(map[*websocket.Conn]string)

// clients map'ine güvenli erişim için mutex		
var clientsMutex = sync.Mutex{}

// Yayınlanacak mesajlar için kanal
var broadcast = make(chan string)

// SQLite veritabanı bağlantısı
var db *sql.DB

// JWT için gizli anahtar
// Bu anahtar, JWT token'larını imzalamak ve doğrulamak için kullanılır
// Gerçek uygulamalarda bu anahtar gizli tutulmalı ve güvenli bir şekilde saklanmalıdır
// Örnek olarak basit bir string kullanıldı, ancak gerçek uygulamalarda daha karmaşık ve güvenli bir anahtar kullanılmalıdır
var jwtKey = []byte("chat_secret_key")

func main() {
	// Veritabanını aç
	var err error
	// SQLite veritabanı bağlantısını oluştur
	// Veritabanı dosyası "./chat.db" olarak ayarlandı
	// Eğer dosya yoksa otomatik olarak oluşturulacak
	// Eğer veritabanı açılamazsa hata loglanır ve uygulama durdurulur
	// Veritabanı bağlantısı defer ile kapanacak, böylece uygulama kapanınca veritabanı da düzgün şekilde kapatılacak
	db, err = sql.Open("sqlite3", "./chat.db")
	if err != nil {
		log.Fatal(err) // Hata olursa uygulamayı durdur
	}
	defer db.Close() // Uygulama kapanınca veritabanını kapat

	// users tablosu yoksa oluştur
	// Bu tablo kullanıcı kayıtları için kullanılır
	// Kullanıcı adı benzersiz olacak şekilde ayarlandı
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		password TEXT
	)`)
	if err != nil {
		log.Fatal(err)
	}

	// messages tablosu yoksa oluştur
	// Bu tablo mesaj kayıtları için kullanılır
	// Her mesajın kullanıcı adı, içeriği ve oluşturulma zamanı kaydedilir
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT,
		message TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		log.Fatal(err)
	}

	// Statik dosyalar için kök dizini ayarla (index.html vs.)	
	http.Handle("/", http.FileServer(http.Dir("./")))

	// WebSocket endpoint'i
	http.HandleFunc("/ws", handleConnections)

	// Kayıt ol endpoint'i
	http.HandleFunc("/register", registerHandler)

	// Giriş yap endpoint'i
	http.HandleFunc("/login", loginHandler)

	// Yayın kanalını dinle
	// Bu fonksiyon, gelen mesajları dinler ve tüm WebSocket bağlantılarına gönderir
	
	go handleMessages()

	fmt.Println("Server http://localhost:8080 üzerinde çalışıyor...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func generateJWT(username string) (string, error) {
	// JWT token oluşturma fonksiyonu
	// Bu fonksiyon, kullanıcı adını alır ve JWT token'ı oluşturur
	// Token, 24 saat geçerli olacak şekilde ayarlanır
	// Token, kullanıcı adını ve oluşturulma zamanını içerir
	// Token, gizli anahtar ile imzalanır ve string olarak döndürülür
	// Eğer token oluşturulamazsa hata döndürülür
	claims := &jwt.RegisteredClaims{
		Subject:   username,                                 // Token sahibinin adı
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)), // Token süresi
		IssuedAt:  jwt.NewNumericDate(time.Now()),           // Token veriliş zamanı
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtKey) // İmzala ve string olarak döndür
}		

func validateJWT(tokenString string) (string, error) {
	// Gelen JWT token'ı doğrulama fonksiyonu
	// Bu fonksiyon, token'ı alır ve geçerliliğini kontrol eder
	// Eğer token geçerliyse kullanıcı adını döndürür, değilse hata döndürür
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		return jwtKey, nil 
	})
	if err != nil || !token.Valid {
		return "", fmt.Errorf("token geçersiz")
	}
	return claims.Subject, nil 
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Yeni WebSocket bağlantısı kur
	// HTTP isteğini WebSocket'e yükselt
	// Eğer yükseltme başarısız olursa hata loglanır ve fonksiyon sonlandırılır
	// Bağlantı başarılı olursa, socket'i kapatmak için defer ilk mesaj olarak JWT token beklenir
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade hatası:", err)
		return
	}
	defer ws.Close() // Bağlantı kapandığında socket'i kapat

	// İlk mesaj JWT token olmalı

	_, msg, err := ws.ReadMessage()
	if err != nil {
		log.Println("Token alınamadı:", err)
		return
	}

	tokenString := string(msg)
	username, err := validateJWT(tokenString)
	if err != nil {
		log.Println("Geçersiz token:", err)
		ws.WriteMessage(websocket.TextMessage, []byte("Geçersiz token!"))
		return
	}

	// Bağlantıyı ve kullanıcı adını kaydet
	// clients map'ine kullanıcı adını ekle
	// clientsMutex kullanarak eşzamanlı erişimi güvenli hale getir
	// Böylece birden fazla goroutine aynı anda clients map'ine erişemez
	// Bu, veri tutarlılığını sağlar ve yarış koşullarını önler
	// Yeni kullanıcı bağlandığında, kullanıcı adını ve bağlantıyı clients map'ine ekle
	log.Printf("Yeni bağlantı: %s", username)
	clientsMutex.Lock()
	clients[ws] = username
	clientsMutex.Unlock()

	// Yeni kullanıcıya hoş geldin mesajı gönder
	broadcast <- fmt.Sprintf("🔵 %s katıldı", username)

	// Son mesajları yeni kullanıcıya gönder
	// Bu, yeni kullanıcıya sohbet geçmişini gösterir
	// Son 10 mesajı veritabanından alır ve WebSocket üzerinden gönderir
	sendLastMessages(ws)

	// Aktif kullanıcı listesini güncelle
	// Bu, tüm kullanıcılara aktif kullanıcı listesini gönderir
	// Böylece herkes kimin çevrimiçi olduğunu görebilir
	sendActiveUsers()

	// Kullanıcıdan gelen mesajları oku
	// Bu döngü, kullanıcının gönderdiği mesajları dinler
	// Eğer kullanıcı bağlantısını keserse, bu döngüden çıkılır
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			// Bağlantı koparsa kullanıcıyı listeden sil
			clientsMutex.Lock()
			delete(clients, ws)
			clientsMutex.Unlock()
			broadcast <- fmt.Sprintf("🔴 %s ayrıldı", username)
			sendActiveUsers()
			break
		}
		// Emoji parser'la emojileri değiştir
		text := emojiParser(string(msg))
		// Zaman damgası ekle
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		// Mesajı yayınla
		broadcast <- fmt.Sprintf("%s: %s [%s]", username, text, timestamp)
	}
}

func handleMessages() {
	// Bu fonksiyon, gelen mesajları alır ve tüm WebSocket bağlantılarına gönderir
	for {
		msg := <-broadcast
		saveMessageToDB(msg)

		clientsMutex.Lock()
		for client := range clients {
			client.WriteMessage(websocket.TextMessage, []byte(msg))
		}
		clientsMutex.Unlock()
	}
}

func saveMessageToDB(msg string) {
	// Bu fonksiyon, gelen mesajı veritabanına kaydeder
	// Mesajın kullanıcı adını ve içeriğini ayıklar
	username := "Sistem"
	message := msg
	if strings.Contains(msg, ": ") {
		parts := strings.SplitN(msg, ": ", 2)
		username = parts[0]
		message = parts[1]
	}
	_, err := db.Exec("INSERT INTO messages(username, message) VALUES(?, ?)", username, message)
	if err != nil {
		log.Println("Mesaj DB'ye kaydedilemedi:", err)
	}
}

func sendLastMessages(ws *websocket.Conn) {
	// Bu fonksiyon, yeni kullanıcıya son 10 mesajı veritabanından alır ve WebSocket üzerinden gönderir

	rows, err := db.Query(`SELECT username, message, created_at FROM messages ORDER BY id DESC LIMIT 10`)
	if err != nil {
		log.Println("Son mesajlar çekilemedi:", err)
		return
	}
	defer rows.Close() 	// Son mesajları ters sırada alır
	// Bu, en son mesajların en üstte olmasını sağlar
	// Böylece yeni kullanıcı en son mesajları ilk olarak görür

	var messages []string	
	for rows.Next() {
		// Her satırı oku ve mesajları listeye ekle
		// Satırları username, message ve created_at olarak ayıkla
		var u, m, t string				
		err := rows.Scan(&u, &m, &t)				
		if err != nil {
			log.Println("Satır okunamadı:", err)
			continue
		}
		messages = append([]string{fmt.Sprintf("%s: %s [%s]", u, m, t)}, messages...)
	}

	for _, msg := range messages {
		ws.WriteMessage(websocket.TextMessage, []byte(msg))
	}
}

func sendActiveUsers() {
	// Bu fonksiyon, tüm aktif kullanıcıları alır ve WebSocket üzerinden gönderir
	// clients map'inden kullanıcı adlarını alır
	var users []string
	clientsMutex.Lock()
	for _, name := range clients {
		users = append(users, name)
	}
	clientsMutex.Unlock()

	data := map[string]interface{}{
		"type":  "active_users",
		"users": users,
	}
	jsonData, _ := json.Marshal(data)

	clientsMutex.Lock()
	for client := range clients {
		client.WriteMessage(websocket.TextMessage, jsonData)
	}
	clientsMutex.Unlock()
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	// Kullanıcı kayıt işlemi
	// Eğer istek POST değilse hata döndür
	// Kullanıcı adı ve şifre boşsa hata döndür
	// Şifreyi hash'le ve veritabanına kaydet
	// Eğer kullanıcı zaten varsa hata döndür
	// Kayıt başarılıysa mesaj döndür
	if r.Method != http.MethodPost {
		http.Error(w, "Sadece POST!", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Error(w, "Boş alan!", http.StatusBadRequest)
		return
	}
	// Kullanıcı adı benzersiz olmalı, bu yüzden veritabanında kontrol et
	var exists int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&exists)
	if err != nil {
		http.Error(w, "Veritabanı hatası!", http.StatusInternalServerError)
		return
	}
	// bcrypt kullanarak şifreyi hash'le
	hashed, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	_, err = db.Exec("INSERT INTO users(username, password) VALUES(?, ?)", username, string(hashed))
	if err != nil {
		http.Error(w, "Kullanıcı var!", http.StatusConflict)
		return
	}
	w.Write([]byte("Kayıt başarılı!"))
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	// Kullanıcı giriş işlemi
	if r.Method != http.MethodPost {
		http.Error(w, "Sadece POST!", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Error(w, "Boş alan!", http.StatusBadRequest)
		return
	}

	var hashed string
	err := db.QueryRow("SELECT password FROM users WHERE username = ?", username).Scan(&hashed)
	if err != nil {
		http.Error(w, "Kullanıcı bulunamadı!", http.StatusUnauthorized)
		return
	}
	err = bcrypt.CompareHashAndPassword([]byte(hashed), []byte(password))
	if err != nil {
		http.Error(w, "Şifre yanlış!", http.StatusUnauthorized)
		return
	}

	token, err := generateJWT(username)
	if err != nil {
		http.Error(w, "Token üretilemedi!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token": token,
	})
}

func emojiParser(text string) string {
	// Yazıdaki belirli keyword'leri emojiye çevir
	replacements := map[string]string{
		":smile:": "😄",
		":heart:": "❤️",
		":fire:":  "🔥",
	}
	for k, v := range replacements {
		text = strings.ReplaceAll(text, k, v)
	}
	return text
}
