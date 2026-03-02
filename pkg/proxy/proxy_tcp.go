package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"syscall"
)

func (p *Proxy) startTcpProxy(ctx context.Context, errChan chan<- error, proxyConfig *TcpProxyConfig) error {
	log := p.log.With(slog.String("proxy", fmt.Sprintf("TCP:%d", proxyConfig.Port)))

	log.Info("started tcp proxy")
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{Port: int(proxyConfig.Port)})
	if err != nil {
		return fmt.Errorf("cannot listen for tcp :%d : %w", proxyConfig.Port, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		for {
			conn, err := listener.AcceptTCP()
			switch {
			case errors.Is(err, syscall.ECONNABORTED):
				log.Error("accepted connection aborted", slog.String("error", err.Error()))
				continue
			// Handle close
			case err != nil:
				log.Error("cannot accept tcp connection", slog.String("error", err.Error()))
				errChan <- err
				cancel()
				return
			}

			p.connectTcp(ctx, log, conn, proxyConfig)
		}
	}()

	go func() {
		<-ctx.Done()
		err := listener.Close()
		if err != nil {
			errChan <- err
		}
		errChan <- nil
	}()

	return nil
}

func (p *Proxy) connectTcp(ctx context.Context, log *slog.Logger, downstreamConn *net.TCPConn, proxyConfig *TcpProxyConfig) {

	tcpAdd := net.TCPAddr{
		IP:   proxyConfig.EndpointAddr.Addr().AsSlice(),
		Port: int(proxyConfig.EndpointAddr.Port()),
	}
	upstreamConn, err := net.DialTCP("tcp", nil, &tcpAdd)
	if err != nil {
		log.Error("cannot connect to tcp proxy", "err", err)
		return
	}
	_, err = upstreamConn.Write(getProxyHeader(downstreamConn))
	if err != nil {
		upstreamConn.Close()
		downstreamConn.Close()
		log.Error("cannot send proxy header", "err", err)
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		_, err := downstreamConn.WriteTo(upstreamConn)
		switch {
		case errors.Is(err, net.ErrClosed):
			break
		case err != nil:
			log.Error("cannot copy from upstream con", "err", err)
		}
		upstreamConn.Close()
		downstreamConn.Close()
		cancel()
	}()
	go func() {
		_, err := upstreamConn.WriteTo(downstreamConn)
		switch {
		case errors.Is(err, net.ErrClosed):
			break
		case err != nil:
			log.Error("cannot copy from downstream con", "err", err)
		}
		upstreamConn.Close()
		downstreamConn.Close()
		cancel()
	}()

	go func() {
		<-ctx.Done()
		downstreamConn.Close()
		upstreamConn.Close()
	}()
}

func getProxyHeader(conn *net.TCPConn) []byte {
	remoteAddr := conn.RemoteAddr().(*net.TCPAddr).AddrPort()
	localAddr := conn.LocalAddr().(*net.TCPAddr).AddrPort()

	builder := strings.Builder{}

	builder.WriteString("PROXY ")

	switch {
	case remoteAddr.Addr().Is6():
		builder.WriteString("TCP6 ")
		builder.WriteString(remoteAddr.Addr().String())
		builder.WriteString(" ")
		builder.WriteString(localAddr.Addr().String())
		builder.WriteString(" ")
		builder.WriteString(strconv.Itoa(int(remoteAddr.Port())))
		builder.WriteString(" ")
		builder.WriteString(strconv.Itoa(int(localAddr.Port())))
		builder.Write([]byte{0x0D, 0x0A})
	case remoteAddr.Addr().Is4():
		builder.WriteString("TCP4 ")
		builder.WriteString(remoteAddr.Addr().String())
		builder.WriteString(" ")
		builder.WriteString(localAddr.Addr().String())
		builder.WriteString(" ")
		builder.WriteString(strconv.Itoa(int(remoteAddr.Port())))
		builder.WriteString(" ")
		builder.WriteString(strconv.Itoa(int(localAddr.Port())))
		builder.Write([]byte{0x0D, 0x0A})
	}

	return []byte(builder.String())
}
