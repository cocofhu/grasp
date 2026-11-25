package sshgateway

import (
	"errors"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// forwardedChannels are relayed to the sandbox sshd unchanged. Remote
// forwarding, X11 and agent channels are refused.
var forwardedChannels = map[string]bool{
	"session":                        true,
	"direct-tcpip":                   true,
	"direct-streamlocal@openssh.com": true,
}

// blockedRequests are channel requests the gateway answers with failure
// instead of relaying: they would expose the user's machine to the sandbox.
var blockedRequests = map[string]bool{
	"auth-agent-req@openssh.com": true,
	"x11-req":                    true,
}

// proxyChannel opens the same channel on upstream and relays it.
func proxyChannel(nc ssh.NewChannel, upstream ssh.Conn) {
	up, upReqs, err := upstream.OpenChannel(nc.ChannelType(), nc.ExtraData())
	if err != nil {
		var oce *ssh.OpenChannelError
		if errors.As(err, &oce) {
			_ = nc.Reject(oce.Reason, oce.Message)
		} else {
			_ = nc.Reject(ssh.ConnectionFailed, "sandbox connection lost")
		}
		return
	}
	down, downReqs, err := nc.Accept()
	if err != nil {
		_ = up.Close()
		go ssh.DiscardRequests(upReqs)
		return
	}
	pipeChannels(down, downReqs, up, upReqs)
}

// pipeChannels relays data, stderr and requests both ways. The client channel
// is closed only after upstream output and its final requests (exit-status)
// were delivered.
func pipeChannels(down ssh.Channel, downReqs <-chan *ssh.Request, up ssh.Channel, upReqs <-chan *ssh.Request) {
	var output sync.WaitGroup
	output.Add(2)
	go func() {
		defer output.Done()
		_, _ = io.Copy(down, up)
	}()
	go func() {
		defer output.Done()
		_, _ = io.Copy(down.Stderr(), up.Stderr())
	}()
	go func() {
		_, _ = io.Copy(up, down)
		_ = up.CloseWrite()
	}()
	upDone := make(chan struct{})
	go func() {
		defer close(upDone)
		forwardRequests(upReqs, down, nil)
		output.Wait()
		_ = down.CloseWrite()
		_ = down.Close()
	}()
	forwardRequests(downReqs, up, blockedRequests)
	_ = up.Close()
	<-upDone
}

func forwardRequests(reqs <-chan *ssh.Request, target ssh.Channel, blocked map[string]bool) {
	for req := range reqs {
		if blocked[req.Type] {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}
		ok, err := target.SendRequest(req.Type, req.WantReply, req.Payload)
		if req.WantReply {
			_ = req.Reply(ok && err == nil, nil)
		}
	}
}
