Writer: Jeronimo Tzib

package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	clientTimeout    = 30*time.Second
	cleanupInterval  = 10*time.Second
	metricsInterval  = 5*time.Second
	serverBufferSize = 2048 //increased buffer size for seq num and timestamp
)

//client represents a connected client with last activity timestamp
type client struct{
	addr       *net.UDPAddr //network address of client
	lastActive time.Time    //last message received time for timeout detection
}

//server maintains shared state for the UDP chat server
type server struct{
	conn *net.UDPConn //UDP network connection

	clients      map[string]*client //map of connected clients (key: addr.String())
	clientsMutex sync.RWMutex       //mutex for concurrent access to clients map

	// Metrics
	receivedMessagesTotal uint64
	receivedBytesTotal    uint64
	metricsMutex          sync.Mutex //to protect aggregated metrics for reading by logMetrics

	// Per-client packet loss tracking (optional, more advanced)
	// clientLastSeq last sequence number seen
	// clientLostPackets count of inferred lost packets
}

//NewServer creates and initializes a new UDP server instance
func NewServer(addr string) (*server, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolving UDP address: %w", err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("listening on UDP: %w", err)
	}
	log.Printf("Server listening on %s", udpAddr.String())

	return &server{
		conn:    conn,
		clients: make(map[string]*client),

	}, nil
}

// Start begins the server's background processing routines
func (s *server) Start(){

	log.Println("Server starting...")
	go s.handleMessages()
	go s.cleanupInactiveClients()
	go s.logMetrics()
}

//handleMessages continuously reads from UDP socket and processes messages
func (s *server) handleMessages(){

	buffer := make([]byte, serverBufferSize) //reusable buffer for incoming datagrams

	for{
		n, addr, err := s.conn.ReadFromUDP(buffer)
		if err != nil{

			//check for listener closed error, common during shutdown
			if strings.Contains(err.Error(), "use of closed network connection"){
				log.Println("Server connection closed, stopping message handler.")
				return
			}
			log.Printf("Read error: %v (from %s)", err, addr)
			continue
		}

		//update server metrics(atomically, though handleMessages is single-threaded for reads)
		atomic.AddUint64(&s.receivedMessagesTotal, 1)
		atomic.AddUint64(&s.receivedBytesTotal, uint64(n))

		msgString := string(buffer[:n])

		//optional: parse message for server-side analysis (e.g., client-to-server latency, seq num tracking)
		parts := strings.SplitN(msgString, "|", 3)
		if len(parts) == 3{
			
			//basic per-client sequence tracking (example, could be expanded)
			//s.clientsMutex.Lock() // Need to protect clientLastSeq if used
			

		} else{
			log.Printf("SrvRcv: Malformed message from %s: %s", addr.String(), msgString)
		}

		s.updateClient(addr) //update or add client
		s.broadcast(msgString, addr) //send to all other clients
	}
}

//updateClient adds/updates a client in the connection pool
func (s *server) updateClient(addr *net.UDPAddr){

	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	key := addr.String()
	if _, exists := s.clients[key]; !exists{
		log.Printf("New client: %s", key)
		s.clients[key] = &client{addr: addr, lastActive: time.Now()}
	} else{
		s.clients[key].lastActive = time.Now()
	}
}

//broadcast sends a message to all connected clients except the sender
func (s *server) broadcast(msg string, excludeAddr *net.UDPAddr){

	s.clientsMutex.RLock() //rad lock for concurrent access

	currentClients := make([]*client, 0, len(s.clients))

	for _, c := range s.clients{

		//ensure we are not sending back to the original sender
		if c.addr.String() != excludeAddr.String(){
			currentClients = append(currentClients, c)
		}
	}
	s.clientsMutex.RUnlock()

	if len(currentClients) == 0{
		return
	}

	data := []byte(msg)
	for _, c := range currentClients{
		//send message concurrently to prevent one slow client from blocking others
		//this can create many goroutines under high load
		//for extreme scale, a worker pool for sending might be considered

		go func(targetAddr *net.UDPAddr){

			_, err := s.conn.WriteToUDP(data, targetAddr)
			if err != nil{
				log.Printf("Broadcast send error to %s: %v", targetAddr, err)
				//optionally, handle repeated send errors by removing the client
				//but we need to be careful as UDP send errors are not always indicative of a dead client.
			}
		}(c.addr)
	}
}

//cleanupInactiveClients periodically removes clients that haven't sent messages
func (s *server) cleanupInactiveClients(){

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C{

		s.clientsMutex.Lock()

		now := time.Now()
		for key, c := range s.clients {
			if now.Sub(c.lastActive) > clientTimeout {
				log.Printf("Client %s timed out. Removing.", key)
				delete(s.clients, key)
			}
		}
		s.clientsMutex.Unlock()
	}
}

//logMetrics periodically logs server performance metrics
func (s *server) logMetrics(){

	ticker := time.NewTicker(metricsInterval)
	defer ticker.Stop()

	var lastMessages uint64
	var lastBytes uint64
	lastTime := time.Now()

	for range ticker.C{

		s.metricsMutex.Lock() //lock for reading totals

		currentMessages := atomic.LoadUint64(&s.receivedMessagesTotal)
		currentBytes := atomic.LoadUint64(&s.receivedBytesTotal)
		s.metricsMutex.Unlock()

		now := time.Now()
		elapsedSeconds := now.Sub(lastTime).Seconds()

		msgRate := 0.0
		byteRate := 0.0
		if elapsedSeconds > 0 {
			msgRate = float64(currentMessages-lastMessages) / elapsedSeconds
			byteRate = float64(currentBytes-lastBytes) / elapsedSeconds
		}

		s.clientsMutex.RLock()
		activeClients := len(s.clients)
		s.clientsMutex.RUnlock()

		log.Printf("Metrics: ActiveClients: %d, MsgsTotal: %d, BytesTotal: %d, MsgRate: %.2f/s, ByteRate: %.2f KB/s",
			activeClients, currentMessages, currentBytes, msgRate, byteRate/1024.0)

		lastMessages = currentMessages
		lastBytes = currentBytes
		lastTime = now
	}
}

//Stop gracefully shuts down the server
func (s *server) Stop(){

	log.Println("Server stopping...")

	if s.conn != nil {
		s.conn.Close() //this will cause ReadFromUDP to return an error, stopping handleMessages
	}
	log.Println("Server stopped.")
}

//Server entry point
func main(){

	srv, err := NewServer("0.0.0.0:8080")

	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	srv.Start()

	// Keep main goroutine alive.
	// In a real app, you'd handle signals for graceful shutdown
	select {}
}
