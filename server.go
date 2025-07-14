package main

import (
    "fmt"
    "log"
    "net/http"

    // Gorilla WebSocket paketi, HTTP bağlantısını WebSocket'e dönüştürmemizi sağlar
    "github.com/gorilla/websocket"
)

// WebSocket bağlantısını yükseltecek yapı
// Tarayıcıdan gelen HTTP isteğini WebSocket'e dönüştürür
var upgrader = websocket.Upgrader{
    // Tarayıcıdan gelen isteğin güvenlik kontrolünü geçmesi için true döner
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

// Bu fonksiyon her yeni WebSocket istemcisi bağlandığında çalışır
func handleConnections(w http.ResponseWriter, r *http.Request) {
    // HTTP bağlantısını WebSocket bağlantısına çevir
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Yükseltme hatası:", err)
        return
    }
    // Bağlantı sona erdiğinde kapat
    defer ws.Close()

    for {
        // Tarayıcıdan mesajı oku
        _, msg, err := ws.ReadMessage()
        if err != nil {
            log.Println("Okuma hatası:", err)
            break
        }

        // Gelen mesajı terminale yazdır
        fmt.Printf("İstemciden mesaj: %s\n", msg)

        // Aynı mesajı tekrar tarayıcıya geri gönder (echo)
        err = ws.WriteMessage(websocket.TextMessage, msg)
        if err != nil {
            log.Println("Gönderim hatası:", err)
            break
        }
    }
}

func main() {
    // "/ws" yoluna gelen istekleri handleConnections fonksiyonuna yönlendiriyoruz.
    http.HandleFunc("/ws", handleConnections)

    // Kök dizindeki (./) tüm dosyaları HTTP üzerinden servis etmek için FileServer kullanıyoruz.
    // Bu, tarayıcıdan client.html ve style.css gibi dosyalara erişim sağlar.
    // Kök dizindeki (./) tüm dosyaları (client.html, style.css) HTTP üzerinden servis et.
    http.Handle("/", http.FileServer(http.Dir("./")))

    // Sunucuyu 8080 portunda başlat
    fmt.Println("WebSocket sunucusu http://localhost:8080/ws adresinde çalışıyor...")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
// Bu kod, basit bir WebSocket sunucusu oluşturur.
// Tarayıcıdan gelen WebSocket bağlantılarını kabul eder ve gelen mesajları terminale yazdırır. 
// Ayrıca, gelen mesajları tarayıcıya geri gönderir (echo özelliği). 
// Sunucu, HTTP isteklerini dinler ve "/ws" yoluna gelen istekleri handleConnections fonksiyonuna yönlendirir. 
// Sunucu, 8080 portunda çalışır.
// Tarayıcıdan WebSocket bağlantısı kurmak için http://localhost:8080/ws adresini kullanabilirsiniz.
