package main

import (
    "fmt"         // Formatlı çıktı işlemleri için
    "log"         // Loglama işlemleri için
    "net/http"    // HTTP sunucusu başlatmak için
    "sync"        // Aynı anda birden çok kullanıcı erişimi için kilitleme mekanizması
    "github.com/gorilla/websocket" // WebSocket bağlantısı için Gorilla paketi
)

// WebSocket bağlantısını HTTP üzerinden yükseltmek için kullanılır
var upgrader = websocket.Upgrader{
    // CORS (tarayıcı kaynak kısıtlamaları) sorunlarını önlemek için izin veriyoruz
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

// Tüm bağlı istemcileri saklayacağımız map (bağlantı listesi)
var clients = make(map[*websocket.Conn]bool)

// Aynı anda farklı kullanıcıların listeye erişmesini güvenli yapmak için mutex (kilit)
var clientsMutex = sync.Mutex{}

// Gelen mesajları geçici olarak tutmak için kanal (channel)
var broadcast = make(chan []byte)

func main() {
    // WebSocket bağlantılarını "/ws" yolunda kabul et
    http.HandleFunc("/ws", handleConnections)

    // client.html ve style.css gibi dosyaları kök dizinden sunmak için
    http.Handle("/", http.FileServer(http.Dir("./")))

    // Mesaj yayınlayıcı fonksiyonu ayrı bir go routine olarak çalıştırılır
    go handleMessages()

    // HTTP sunucusunu 8080 portunda başlat
    fmt.Println("WebSocket sunucusu http://localhost:8080/ws adresinde çalışıyor...")
    log.Fatal(http.ListenAndServe(":8080", nil))
}

// Bu fonksiyon her yeni WebSocket istemcisi bağlandığında çalışır
func handleConnections(w http.ResponseWriter, r *http.Request) {
    // HTTP bağlantısını WebSocket'e yükselt
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Yükseltme hatası:", err)
        return
    }
    // Fonksiyon bittiğinde bağlantıyı kapat
    defer ws.Close()

    // Bağlanan istemciyi listeye ekliyoruz
    clientsMutex.Lock()
    clients[ws] = true
    clientsMutex.Unlock()

    for {
        // Kullanıcıdan gelen mesajı oku
        _, msg, err := ws.ReadMessage()
        if err != nil {
            log.Println("Bağlantı kesildi:", err)

            // Kullanıcı bağlantısı kopunca listeden çıkar
            clientsMutex.Lock()
            delete(clients, ws)
            clientsMutex.Unlock()
            break
        }

        // Gelen mesajı broadcast kanalına gönder (tüm istemcilere iletilecek)
        broadcast <- msg
    }
}

// Bu fonksiyon gelen her mesajı tüm bağlı istemcilere dağıtır
func handleMessages() {
    for {
        // Kanal üzerinden mesajı al
        msg := <-broadcast

        // clients listesine erişmeden önce kilitliyoruz
        clientsMutex.Lock()

        // Tüm istemcilerde döngü yap
        for client := range clients {
            // Her istemciye mesajı gönder
            err := client.WriteMessage(websocket.TextMessage, msg)
            if err != nil {
                log.Println("Gönderim hatası:", err)
                client.Close()
                delete(clients, client)
            }
        }

        // clients erişimini serbest bırak
        clientsMutex.Unlock()
    }
}
// Bu kod, basit bir WebSocket sunucusu oluşturur ve istemciler arasında gerçek zamanlı mesajlaşma sağlar.
// İstemciler, "/ws" yoluna bağlanarak WebSocket bağlantısı kurabilir ve mesaj gönderebilir.
// Sunucu, gelen mesajları tüm bağlı istemcilere iletir.
// Ayrıca, istemciler arasında bağlantı kesildiğinde otomatik olarak temizleme işlemi yapılır.
