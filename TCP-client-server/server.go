package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type Message struct {
	name   string
	text   string
	sender net.Conn
}

var (
	clients   = make(map[net.Conn]bool)
	names     = make(map[net.Conn]string)
	mutex     sync.Mutex
	broadcast = make(chan Message)
)

func handleConnection(conn net.Conn) {
	defer conn.Close()

	//ask for and read username
	fmt.Fprint(conn, "Enter your name: ")
	nameReader := bufio.NewReader(conn)

	//set 30 sec disconnect for inactvity 
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	rawName, err := nameReader.ReadString('\n')
	if err != nil {
		return
	}
	conn.SetReadDeadline(time.Time{}) //clear the timer after successful read

	username := strings.TrimSpace(rawName)
	if username == "" {
		username = "Anonymous"
	}

	//get the current login time
	loginTime := time.Now().Format("15:04:05")

	//register client
	mutex.Lock()
	clients[conn] = true
	names[conn] = username
	mutex.Unlock()

	//announce join
	broadcast <- Message{
		name:   "Server",
		text:   fmt.Sprintf("%s has joined the chat at %s\n", username, loginTime),
		sender: nil,
	}
	inactive := false 
	//read loop
	reader := bufio.NewReader(conn)
	for {
		//reset deadline on each loop
		conn.SetReadDeadline(time.Now().Add(30 * time.Second)) 
		msg, err := reader.ReadString('\n')
		if err != nil {
			//inactivity timeout or connection error
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Printf("%s has been disconnected due to inactivity\n", username)
				broadcast <- Message{
					name:   "Server",
					text:   fmt.Sprintf("%s has been disconnected due to inactivity.\n", username),
					sender: nil,
				}
				inactive = true
			}
			break
		}
		msg = strings.TrimSpace(msg)

		if len(msg) > 100 {
    	msg = msg[:100] // truncate
    	fmt.Fprintln(conn, "[Server] Your message was too long and has been truncated to 100 characters.")
}

		if msg == "" {
			continue // Skip empty messages
		}
		
		conn.SetReadDeadline(time.Time{}) //clear deadline after read
		broadcast <- Message{
			name:   username,
			text:   msg,
			sender: conn,
		}
	}
		if !inactive {
		//get current disconnect time
		disconnectTime := time.Now().Format("15:04:05")
		mutex.Lock()
		delete(clients, conn)
		delete(names, conn)
		mutex.Unlock()

		broadcast <- Message{
			name:   "Server",
			text:   fmt.Sprintf("%s has left the chat at %s\n", username, disconnectTime),
			sender: nil,
		}
	} else {
		//client disconnects
		mutex.Lock()
		delete(clients, conn)
		delete(names, conn)
		mutex.Unlock()
	}
}

func broadcaster() {
	for {
		m := <-broadcast
		start := time.Now() //start timer for latency
		mutex.Lock()
		for conn := range clients {
			//skip echoing back to the sender when its a user message
			if m.sender != nil && conn == m.sender {
				continue
			}
			//prefix server messages with [Server] and others with [username]
			prefix := fmt.Sprintf("[%s] ", m.name)
			messageText := prefix + m.text
			fmt.Fprint(conn, messageText)

			//log message to file named after receiver's IP address
			clientAddr := conn.RemoteAddr().String()
			clientIP := strings.Split(clientAddr, ":")[0]
			filename := fmt.Sprintf("%s.log", clientIP)

			go func(entry, file string) {
				f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err == nil {
					defer f.Close()
					f.WriteString(entry)
				}
			}(messageText, filename)
		}
		mutex.Unlock()

		elapsed := time.Since(start) //end timer
		fmt.Printf("Broadcast latency: %v\n", elapsed) //log latency
	}
}


func main() {
	listener, err := net.Listen("tcp", ":4000")
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	go broadcaster()
	fmt.Println("TCP chat server started on port 4000...")

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleConnection(conn)
	}
}
