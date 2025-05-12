package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
	"runtime"
	"flag"
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

//metrics struct to track server performance
type Metrics struct {
	mu             sync.Mutex
	messageCount   int64
	totalLatency   time.Duration
	startTime      time.Time
	droppedPackets int64
}

var metrics = Metrics{
	startTime: time.Now(),
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	//10 messages/second max per client
	rateLimiter := time.NewTicker(100 * time.Millisecond)
	defer rateLimiter.Stop()

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
			continue //skip empty messages
		}
		if strings.HasPrefix(msg, "/") {
			switch msg {
			case "/quit":
				fmt.Fprintln(conn, "[Server] Goodbye!")
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
				return //close the connection
			default:
				fmt.Fprintf(conn, "[Server] Unknown command: %s\n", msg)
			}
			continue //skip broadcasting commands
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
    //create a buffered channel for batch processing
    messageBatch := make(chan []Message, 100)
    defer close(messageBatch)

    //start a worker to handle batch sending
    go func() {
        for batch := range messageBatch {
			start := time.Now()
            mutex.Lock()
            
            //process all messages in the batch
            for _, msg := range batch {
                //log messages (same as before)
                if msg.sender != nil {
                    ip := strings.Split(msg.sender.RemoteAddr().String(), ":")[0]
                    logFile, err := os.OpenFile(ip+".log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
                    if err == nil {
                        logFile.WriteString(fmt.Sprintf("[%s] %s\n", msg.name, msg.text))
                        logFile.Close()
                    }
                } else {
                    for client := range clients {
                        ip := strings.Split(client.RemoteAddr().String(), ":")[0]
                        logFile, err := os.OpenFile(ip+".log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
                        if err == nil {
                            logFile.WriteString(fmt.Sprintf("[Server] %s", msg.text))
                            logFile.Close()
                        }
                    }
                }

                //send to all clients except sender
                for client := range clients {
                    if client == msg.sender {
                        continue
                    }
                    client.SetWriteDeadline(time.Now().Add(1 * time.Second))
                    fmt.Fprintf(client, "[%s] %s\n", msg.name, msg.text)
                    client.SetWriteDeadline(time.Time{})
                }
            }
            metrics.mu.Lock()
			metrics.messageCount += int64(len(batch))
			metrics.totalLatency += time.Since(start)
			metrics.mu.Unlock()
            mutex.Unlock()
        }
    }()

    //batch messages for processing
    var batch []Message
    batchTimer := time.NewTicker(100 * time.Millisecond) //adjust timing as needed
    defer batchTimer.Stop()

    for {
        select {
        case msg := <-broadcast:
            batch = append(batch, msg)
            
            //send batch if it reaches a certain size
            if len(batch) >= 50 { //adjust batch size as needed
                messageBatch <- batch
                batch = nil
            }

        case <-batchTimer.C:
            //send whatever we have at regular intervals
            if len(batch) > 0 {
                messageBatch <- batch
                batch = nil
            }
        }
    }
}
func main() {
	port := flag.String("port", "4000", "Port number for the chat server")
	maxConn := flag.Int("max", 10, "Maximum concurrent connections")
	flag.Parse()

    //create a connection counter that allows max 10 connections
    connectionLimit := make(chan struct{}, *maxConn)
    defer close(connectionLimit)

    //start a background job to show server stats every minute
    go func() {
        for range time.Tick(30 * time.Second) {
			metrics.mu.Lock()
			duration := time.Since(metrics.startTime).Seconds()
			throughput := float64(metrics.messageCount) / duration
			avgLatency := metrics.totalLatency.Seconds() / float64(metrics.messageCount) * 1000
			lossRate := float64(metrics.droppedPackets) / float64(metrics.droppedPackets+metrics.messageCount) * 100

			mutex.Lock()
			fmt.Printf("\n[Metrics] Connections: %d | Latency: %.2fms | Loss: %.2f%% | Throughput: %.2f msg/s | Goroutines: %d\n",
				len(clients),
				avgLatency,
				lossRate,
				throughput,
				runtime.NumGoroutine())
			mutex.Unlock()

			metrics.messageCount = 0
			metrics.totalLatency = 0
			metrics.droppedPackets = 0
			metrics.startTime = time.Now()
			metrics.mu.Unlock()
		}
	}()

    //start listening on port 4000
    listener, err := net.Listen("tcp", ":"+*port)
    if err != nil {
        panic(err)
    }
    defer listener.Close()

    //start the message broadcaster
    go broadcaster()
    fmt.Printf("Chat server running on port %s (max %d connections)\n", *port, *maxConn)

    //keep accepting new connections
    for {
        //reserve a connection slot (blocks if 1000 already connected)
        connectionLimit <- struct{}{}
        
        //wait for new connection
        conn, err := listener.Accept()
        if err != nil {
            <-connectionLimit //free slot if error occurs
            continue
        }

        //handle the connection in a new goroutine
        go func(c net.Conn) {
            defer func() { <-connectionLimit }() //free slot when done
            handleConnection(c)
        }(conn)
    }
}