//writer: Jeronimo Tzib

package main

import (
	"fmt"
	"log"
	"net"
	"strconv"
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
	lastSeenSeqNum	uint64 //last sequence number received from this client
	expectedNextSeqNum uint64 //next sequence number expected (lastSeen + 1)
	lostPackets uint64 //count of inferred lost packets from this client
	firstMessageReceived bool //to handle the very first message's sequence number
}

//server maintains shared state for the UDP chat server
type server struct{
	conn *net.UDPConn //UDP network connection

	clients      map[string]*client //map of connected clients
	clientsMutex sync.RWMutex //mutex for concurrent access to clients map

	// Metrics
	receivedMessagesTotal uint64
	receivedBytesTotal    uint64
	metricsMutex          sync.Mutex //to protect aggregated metrics for reading by logMetrics

}

//NewServer creates and initializes a new UDP server instance
func NewServer(addr string) (*server, error){
	
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

		//update server metrics(atomically, though handleMessages is single threaded for reads)
		atomic.AddUint64(&s.receivedMessagesTotal, 1)
		atomic.AddUint64(&s.receivedBytesTotal, uint64(n))

		msgString := string(buffer[:n])
		clientKey := addr.String() //get client key for map access

		//packet loss logic
		s.clientsMutex.Lock() //full lock needed to read and potentially write to client struct

		c, exists := s.clients[clientKey]
		if !exists{
            //client is new, will be fully initialized in updateClient
            //for now, just release lock and proceed to updateClient
            //updateClient will create the client entry
            //packet loss tracking will effectively start from the *next* message

		} else{

			parts := strings.SplitN(msgString, "|", 3)

			if len(parts) >= 1 { //we need at least the sequence number part
				seqNumStr := parts[0]
				currentSeqNum, parseErr := strconv.ParseUint(seqNumStr, 10, 64)

				if parseErr == nil{
					if !c.firstMessageReceived{

						//this is the first message we're processing for sequence tracking for this client
						c.firstMessageReceived = true
						c.lastSeenSeqNum = currentSeqNum
						c.expectedNextSeqNum = currentSeqNum + 1
						
					} else{
						if currentSeqNum == c.expectedNextSeqNum{

							//packet arrived as expected
							c.lastSeenSeqNum = currentSeqNum
							c.expectedNextSeqNum = currentSeqNum + 1

						} else if currentSeqNum > c.expectedNextSeqNum{

							//gap detected, packets were lost
							lostCount := currentSeqNum - c.expectedNextSeqNum
							c.lostPackets += lostCount
							log.Printf("PKTLOSS [%s]: Gap detected! Expected seq %d, got %d. Lost %d packet(s). Total lost: %d",
								clientKey, c.expectedNextSeqNum, currentSeqNum, lostCount, c.lostPackets)
							c.lastSeenSeqNum = currentSeqNum
							c.expectedNextSeqNum = currentSeqNum + 1
						} else{

							//currentSeqNum < c.expectedNextSeqNum: Duplicate or out-of-order packet
							//for simplicity, UDP often just logs this and moves on, or ignores old packets
							//we won't count it as "new" loss, but we update lastSeen if it's newer than what we have

                            if currentSeqNum > c.lastSeenSeqNum{ //if it's an out-of-order but newer packet
                                c.lastSeenSeqNum = currentSeqNum
                                //we don't change expectedNextSeqNum based on an out-of-order older packet.
                            }
							log.Printf("PKTLOSS [%s]: Out-of-order/duplicate. Expected %d, got %d. Last seen: %d",
								clientKey, c.expectedNextSeqNum, currentSeqNum, c.lastSeenSeqNum)
						}
					}
				} else{
					log.Printf("PKTLOSS [%s]: Could not parse sequence number from message: %s", clientKey, msgString)
				}
			} else{
				log.Printf("PKTLOSS [%s]: Message too short for sequence number: %s", clientKey, msgString)
			}
		}
		s.clientsMutex.Unlock() //unlock after accessing/modifying client struct


		s.updateClient(addr, msgString) //pass msgString to parse seq num for new clients
		s.broadcast(msgString, addr)
	}
}

//updateClient adds/updates a client in the connection pool
func (s *server) updateClient(addr *net.UDPAddr,msgString string){

	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	key := addr.String()
	now := time.Now()

	if c, exists := s.clients[key]; !exists{

		log.Printf("New client: %s", key)

		newClient := &client{

			addr: addr,
			lastActive: now,

			//initialize packet loss tracking fields
			firstMessageReceived: false, //will be set true on first processed message in handleMessages
			//or, try to parse sequence from this very first message
		}

		//attempt to set initial sequence from the first message
		parts := strings.SplitN(msgString, "|", 3)
		if len(parts) >= 1{

			seqNumStr := parts[0]
			initialSeqNum, parseErr := strconv.ParseUint(seqNumStr, 10, 64)
			if parseErr == nil{

				newClient.lastSeenSeqNum = initialSeqNum
				newClient.expectedNextSeqNum = initialSeqNum + 1
				newClient.firstMessageReceived = true //since we processed its first seq num here
				log.Printf("PKTLOSS [%s]: New client, first message seq %d. Expecting %d.", key, initialSeqNum, newClient.expectedNextSeqNum)
			}
		}
		s.clients[key] = newClient

	} else{
		c.lastActive = now //update existing client's activity time
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
			if now.Sub(c.lastActive) > clientTimeout{
				log.Printf("Client %s timed out. Removing. Total inferred lost packets from this client: %d", key, c.lostPackets) //log lost packets on timeout
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
		var totalLostPacketsAggregated uint64
		for _, cl := range s.clients{
			totalLostPacketsAggregated += cl.lostPackets
		}

		s.clientsMutex.RUnlock()

		log.Printf("Metrics: ActiveClients: %d, MsgsTotal: %d, BytesTotal: %d, MsgRate: %.2f/s, ByteRate: %.2f KB/s, TotalLostPkts(SrvView): %d",
			activeClients, currentMessages, currentBytes, msgRate, byteRate/1024.0, totalLostPacketsAggregated)

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

	//keep main goroutine alive.
	select {}
}
