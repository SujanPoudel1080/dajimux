package peer

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

func TestPeerHandshakeSuccess(t *testing.T) {
	token := "supersecret123"
	serverNode := "server-box"
	clientNode := "laptop"

	tunnelConnected := make(chan string, 1)

	server := NewServer("127.0.0.1:0", token, serverNode, func(peerNode string, conn net.Conn) {
		tunnelConnected <- peerNode
		// Read a message from client
		env, err := protocol.ReadEnvelope(conn)
		if err != nil {
			t.Errorf("server read envelope: %v", err)
			return
		}
		// Echo it back
		_ = protocol.WriteEnvelope(conn, *env)
	})

	if err := server.Listen(); err != nil {
		t.Fatalf("server.Listen() error: %v", err)
	}
	defer server.Close()

	go func() {
		_ = server.Serve()
	}()

	client := NewClient(server.ListenerAddr(), token, clientNode)
	conn, hs, err := client.Connect()
	if err != nil {
		t.Fatalf("client.Connect() failed: %v", err)
	}
	defer conn.Close()

	if hs.NodeName != serverNode {
		t.Fatalf("expected remote node name %q, got %q", serverNode, hs.NodeName)
	}

	select {
	case peer := <-tunnelConnected:
		if peer != clientNode {
			t.Errorf("expected tunnel peer node %q, got %q", clientNode, peer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tunnel connection")
	}

	// Test protocol message round-trip through tunnel
	testPayload := []byte(`{"hello":"world"}`)
	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgInput,
		Payload: testPayload,
	}); err != nil {
		t.Fatalf("failed to send test envelope: %v", err)
	}

	echoEnv, err := protocol.ReadEnvelope(conn)
	if err != nil {
		t.Fatalf("failed to read echo envelope: %v", err)
	}

	if echoEnv.Type != protocol.MsgInput || !bytes.Equal(echoEnv.Payload, testPayload) {
		t.Fatalf("unexpected echo envelope: %+v", echoEnv)
	}
}

func TestPeerHandshakeAuthFailure(t *testing.T) {
	server := NewServer("127.0.0.1:0", "correct-token", "server-box", nil)
	if err := server.Listen(); err != nil {
		t.Fatalf("server.Listen() error: %v", err)
	}
	defer server.Close()

	go func() {
		_ = server.Serve()
	}()

	client := NewClient(server.ListenerAddr(), "wrong-token", "attacker")
	conn, hs, err := client.Connect()
	if err == nil {
		if conn != nil {
			conn.Close()
		}
		t.Fatalf("expected auth error with invalid token, got success (remote hs: %+v)", hs)
	}
}

func TestPeerDialUnreachable(t *testing.T) {
	client := NewClient("127.0.0.1:59999", "tok", "client")
	_, _, err := client.Connect()
	if err == nil {
		t.Fatal("expected dial error connecting to non-existent address, got nil")
	}
}

func TestTunnelToUnix(t *testing.T) {
	// Create mock Unix listener
	tmpDir := t.TempDir()
	unixPath := tmpDir + "/test_daemon.sock"

	unixLn, err := net.Listen("unix", unixPath)
	if err != nil {
		t.Fatalf("failed to listen on unix socket: %v", err)
	}
	defer unixLn.Close()

	go func() {
		uConn, err := unixLn.Accept()
		if err != nil {
			return
		}
		defer uConn.Close()
		// Simple echo server on unix socket
		_, _ = io.Copy(uConn, uConn)
	}()

	// Create TCP server that bridges to unix socket using TunnelToUnix
	server := NewServer("127.0.0.1:0", "tunnel-tok", "server", func(peerNode string, conn net.Conn) {
		_ = TunnelToUnix(conn, unixPath)
	})

	if err := server.Listen(); err != nil {
		t.Fatalf("server.Listen() error: %v", err)
	}
	defer server.Close()

	go func() {
		_ = server.Serve()
	}()

	client := NewClient(server.ListenerAddr(), "tunnel-tok", "client")
	peerConn, _, err := client.Connect()
	if err != nil {
		t.Fatalf("client.Connect() failed: %v", err)
	}
	defer peerConn.Close()

	msg := []byte("ping via dajimux tunnel")
	if _, err := peerConn.Write(msg); err != nil {
		t.Fatalf("failed to write to peer conn: %v", err)
	}

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(peerConn, buf); err != nil {
		t.Fatalf("failed to read echo from peer conn: %v", err)
	}

	if !bytes.Equal(buf, msg) {
		t.Fatalf("expected echo %q, got %q", string(msg), string(buf))
	}
}
