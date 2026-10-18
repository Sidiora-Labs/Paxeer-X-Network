package auth

import (
	"context"
	"errors"
	"github.com/gorilla/websocket"
	"time"
)

func (s *Identity) EnforcePrivateSession(ctx context.Context, p Principal, closeConnection func() error) error {
	if closeConnection == nil {
		return invalid()
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		check, cancel := context.WithTimeout(ctx, time.Second)
		err := s.CheckSocket(check, p)
		cancel()
		if err != nil {
			if closeErr := closeConnection(); closeErr != nil {
				s.observe("private_socket_close_failed")
			}
			s.observe("private_socket_authority_closed")
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Identity) EnforceWebSocket(ctx context.Context, p Principal, connection *websocket.Conn) error {
	if connection == nil {
		return invalid()
	}
	return s.EnforcePrivateSession(ctx, p, func() error {
		writeErr := connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session unavailable"), time.Now().Add(time.Second))
		closeErr := connection.Close()
		return errors.Join(writeErr, closeErr)
	})
}
