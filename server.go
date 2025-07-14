package main

import (
    "fmt"
    "log"
    "net/http"
    "sync"
    "github.com/gorilla/websocket" 
)

// WebSocket bağlantısını HTTP üzerinden yükseltmek için kullanılır
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

// Bağlantı: kullanıcı eşlemesi için bir harita (her istemcinin kullanıcı adını saklarız)
var clients = make(map[*websocket.Conn]string)

// Erişimleri güvenli hale getirmek için mutex
var clientsMutex = sync.Mutex{}

// Mesajları göndermek için kanal
var broadcast = make(chan string) // Artık []byte değil, doğrudan string mesaj gönderiyoruz

func main() {
    http.HandleFunc("/ws", handleConnections)
    http.Handle("/", http.FileServer(http.Dir("./")))

    // Mesajları dağıtan fonksiyonu başlat
    go handleMessages()

    fmt.Println("WebSocket sunucusu http://localhost:8080/ws adresinde çalışıyor...")
    log.Fatal(http.ListenAndServe(":8080", nil))
}

// Bu fonksiyon her yeni bağlantı için çalışır
func handleConnections(w http.ResponseWriter, r *http.Request) {
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Yükseltme hatası:", err)
        return
    }
    defer ws.Close()

    var username string

    // İlk gelen mesajı kullanıcı adı olarak al
    _, firstMsg, err := ws.ReadMessage()
    if err != nil {
        log.Println("İlk mesaj (kullanıcı adı) okunamadı:", err)
        return
    }
    username = string(firstMsg)

    // Kullanıcıyı clients map’ine ekle
    clientsMutex.Lock()
    clients[ws] = username
    clientsMutex.Unlock()

    // Herkese bu kişinin katıldığını duyur
    joinMsg := fmt.Sprintf("🔵 %s katıldı", username)
    broadcast <- joinMsg

    for {
        // Yeni mesajları oku
        _, msg, err := ws.ReadMessage()
        if err != nil {
            // Eğer bağlantı koparsa ayrılma mesajı gönder
            log.Printf("%s bağlantı koptu: %v\n", username, err)

            clientsMutex.Lock()
            delete(clients, ws)
            clientsMutex.Unlock()

            leaveMsg := fmt.Sprintf("🔴 %s ayrıldı", username)
            broadcast <- leaveMsg
            break
        }

        // Kullanıcının mesajını al ve ismiyle birlikte yay
        fullMsg := fmt.Sprintf("%s: %s", username, msg)
        broadcast <- fullMsg
    }
}

// Gelen her mesajı tüm istemcilere gönderir
func handleMessages() {
    for {
        msg := <-broadcast

        clientsMutex.Lock()
        for client := range clients {
            err := client.WriteMessage(websocket.TextMessage, []byte(msg))
            if err != nil {
                log.Println("Mesaj gönderilemedi:", err)
                client.Close()
                delete(clients, client)
            }
        }
        clientsMutex.Unlock()
    }
}