package peer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

// Handshake exchanges identity and authentication between peers.
type Handshake struct {
	NodeName string `json:"node_name"`
	Token    string `json:"token"`
	Version  string `json:"version"`
}

// Server exposes local daemon sessions to authenticated remote peers.
type Server struct {
	mu        sync.RWMutex
	Addr      string
	Token     string
	NodeName  string
	listener  net.Listener
	isClosing bool
	OnTunnel  func(peerNode string, conn net.Conn)
}

// NewServer creates a peering listener for cross-machine access.
func NewServer(addr, token, nodeName string, onTunnel func(string, net.Conn)) *Server {
	if nodeName == "" {
		nodeName = "remote-node"
	}
	return &Server{
		Addr:     addr,
		Token:    token,
		NodeName: nodeName,
		OnTunnel: onTunnel,
	}
}

// Listen binds to the network address without entering the accept loop.
func (s *Server) Listen() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("peer listen: %w", err)
	}
	s.listener = ln
	return nil
}

// ListenerAddr returns the resolved network address of the listener.
func (s *Server) ListenerAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.Addr
}

// Serve accepts incoming connections on the bound listener.
func (s *Server) Serve() error {
	s.mu.RLock()
	ln := s.listener
	s.mu.RUnlock()

	if ln == nil {
		return errors.New("listener not initialized; call Listen() first")
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.RLock()
			closing := s.isClosing
			s.mu.RUnlock()
			if closing {
				return nil
			}
			return err
		}

		go s.handlePeer(conn)
	}
}

// ListenAndServe binds and accepts remote peer connections.
func (s *Server) ListenAndServe() error {
	s.mu.RLock()
	ln := s.listener
	s.mu.RUnlock()

	if ln == nil {
		if err := s.Listen(); err != nil {
			return err
		}
	}
	return s.Serve()
}

func (s *Server) handlePeer(conn net.Conn) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	env, err := protocol.ReadEnvelope(conn)
	if err != nil {
		return
	}

	var hs Handshake
	if err := json.Unmarshal(env.Payload, &hs); err != nil {
		return
	}

	if s.Token != "" && hs.Token != s.Token {
		_ = protocol.WriteEnvelope(conn, protocol.Envelope{
			Type:    protocol.MsgError,
			Payload: []byte(`"unauthorized: invalid peer token"`),
		})
		return
	}

	// Ack handshake
	respData, _ := json.Marshal(Handshake{
		NodeName: s.NodeName,
		Version:  "0.1.0",
	})
	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgAttach,
		Payload: respData,
	}); err != nil {
		return
	}

	_ = conn.SetDeadline(time.Time{})

	if s.OnTunnel != nil {
		s.OnTunnel(hs.NodeName, conn)
	}
}

// Close stops the peer listener.
func (s *Server) Close() error {
	s.mu.Lock()
	s.isClosing = true
	ln := s.listener
	s.mu.Unlock()

	if ln != nil {
		return ln.Close()
	}
	return nil
}

// TunnelToUnix proxies bidirectional traffic between a peer connection and a unix socket.
func TunnelToUnix(peerConn net.Conn, unixSocketPath string) error {
	localConn, err := net.Dial("unix", unixSocketPath)
	if err != nil {
		return fmt.Errorf("dial local unix socket: %w", err)
	}
	defer localConn.Close()

	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(localConn, peerConn)
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(peerConn, localConn)
		errCh <- err
	}()

	<-errCh
	return nil
}

// Client connects to a remote dajimux peer server.
type Client struct {
	Addr     string
	Token    string
	NodeName string
}

// NewClient creates a peer connector.
func NewClient(addr, token, nodeName string) *Client {
	if nodeName == "" {
		nodeName = "local-client"
	}
	return &Client{
		Addr:     addr,
		Token:    token,
		NodeName: nodeName,
	}
}

// Connect dials the remote peer and performs handshake authentication.
func (c *Client) Connect() (net.Conn, *Handshake, error) {
	conn, err := net.DialTimeout("tcp", c.Addr, 5*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("dial peer %s: %w", c.Addr, err)
	}

	hsData, _ := json.Marshal(Handshake{
		NodeName: c.NodeName,
		Token:    c.Token,
		Version:  "0.1.0",
	})

	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgAttach,
		Payload: hsData,
	}); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("write handshake: %w", err)
	}

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	respEnv, err := protocol.ReadEnvelope(conn)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("read handshake response: %w", err)
	}

	if respEnv.Type == protocol.MsgError {
		conn.Close()
		return nil, nil, fmt.Errorf("auth failed: %s", string(respEnv.Payload))
	}

	var remoteHS Handshake
	if err := json.Unmarshal(respEnv.Payload, &remoteHS); err != nil {
		conn.Close()
		return nil, nil, errors.New("invalid handshake payload")
	}

	_ = conn.SetDeadline(time.Time{})
	return conn, &remoteHS, nil
}
