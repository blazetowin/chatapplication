package main

import (
	"fmt" // fmt paketi, formatlı I/O işlemleri için kullanılır
	"log" // log paketi, hata ve bilgi mesajlarını kaydetmek için kullanılır
	"net/http" // net/http paketi, HTTP sunucusu ve istemcisi oluşturmak için kullanılır
	"strings" // strings paketi, string işlemleri için kullanılır
	"sync" // sync paketi, eşzamanlı programlama için kullanılır

	"github.com/gorilla/websocket" // gorilla/websocket paketi, WebSocket bağlantıları için kullanılır
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
	// Kullanıcıdan gelen ilk mesajı alır ve kullanıcı adını belirler
	// Eğer hata oluşursa, hata mesajı yazdırılır ve fonksiyondan çıkılır
	// Kullanıcı adı, clients haritasına kaydedilir
	// clientsMutex kullanılarak eşzamanlı erişim sağlanır
	// Katılım mesajı broadcast kanalına gönderilir
	// Aktif kullanıcı listesi güncellenir ve tüm istemcilere gönderilir		
	_, msg, err := ws.ReadMessage()
	if err != nil {
		log.Println("Kullanıcı adı alınamadı:", err)
		return
	}
	// Kullanıcı adını mesajdan alır
	username = string(msg)
	log.Printf("Yeni kullanıcı: %s\n", username)
	// Kullanıcı adının boş olmadığından emin olun
	if strings.TrimSpace(username) == "" {
		log.Println("Kullanıcı adı boş olamaz")
		return
	}
	// Yeni kullanıcıyı clients haritasına ekler
	// clientsMutex kullanılarak eşzamanlı erişim sağlanır
	clientsMutex.Lock()
	// Eğer kullanıcı adı zaten varsa, hata mesajı yazdırılır ve fonksiyondan çıkılır
	for _, existingUsername := range clients {
		if existingUsername == username {
			log.Printf("Kullanıcı adı zaten kullanılıyor: %s\n", username)
			ws.WriteMessage(websocket.TextMessage, []byte("Kullanıcı adı zaten kullanılıyor"))
			ws.Close()
			clientsMutex.Unlock()
			// Kullanıcı adı zaten kullanılıyorsa, fonksiyondan çıkılır	
			return
		}
	}
	// Yeni kullanıcıyı clients haritasına ekler
	// clients haritasında WebSocket bağlantısını ve kullanıcı adını saklar	
	clients[ws] = username

	// clientsMutex kilidi serbest bırakılır
	clientsMutex.Unlock()

	// Katılım mesajı
	broadcast <- fmt.Sprintf("🔵 %s katıldı", username)

	// Aktif kullanıcı listesini gönder
	sendActiveUsers()

	for {
		// Kullanıcıdan gelen mesajları dinler
		// Eğer hata oluşursa, kullanıcı ayrıldı mesajı yazdırılır
		// clients haritasından kullanıcı silinir
		// broadcast kanalına ayrılma mesajı gönderilir
		// Aktif kullanıcı listesi güncellenir ve tüm istemcilere gönderilir
		// Eğer mesaj alınamazsa, döngüden çıkılır	
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
		// Gelen mesajı emojiParser fonksiyonu ile işleyerek metni dönüştürür
		// Ardından, mesajı broadcast kanalına gönderir
		// Bu mesaj, tüm istemcilere iletilecektir
		text := emojiParser(string(msg))
		broadcast <- fmt.Sprintf("%s: %s", username, text)
	}
}

func handleMessages() {

	for {
		// broadcast kanalından gelen mesajları dinler
		// clients haritasındaki tüm WebSocket bağlantılarına mesajı gönderir		
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
// sendActiveUsers fonksiyonu, aktif kullanıcı listesini oluşturur
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
// emojiParser fonksiyonu, metindeki basit emojileri dönüştürür
// Örneğin, ":smile:" ifadesini "😄" ile değiştirir
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