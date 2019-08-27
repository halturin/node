package ergonode

import (
	"context"

	"github.com/halturin/ergonode/etf"
	"github.com/halturin/ergonode/lib"
)

const (
	startPID = 1000
)

type registerProcessRequest struct {
	name   string
	object interface{}
	opts   map[string]interface{}
}

type registerNameRequest struct {
	name string
	pid  etf.Pid
}

type registerPeer struct {
	name string
	p    peer
}

type registrarChannels struct {
	process           chan registerProcessRequest
	unregisterProcess chan etf.Pid
	name              chan registerNameRequest
	unregisterName    chan string
	peer              chan registerPeer
	unregisterPeer    chan string
	reply             chan *Process
}

type registrar struct {
	nextPID  uint32
	nodeName string
	creation byte

	node *Node

	channels registrarChannels

	names     map[string]etf.Pid
	processes map[etf.Pid]*Process
	peers     map[string]peer
}

func createRegistrar(node *Node) *registrar {
	r := registrar{
		nextPID:  startPID,
		nodeName: node.FullName,
		creation: byte(1),
		node:     node,
		channels: registrarChannels{
			process:           make(chan registerProcessRequest),
			unregisterProcess: make(chan etf.Pid),
			name:              make(chan registerNameRequest),
			unregisterName:    make(chan string),
			peer:              make(chan registerPeer),
			unregisterPeer:    make(chan string),
			reply:             make(chan *Process),
		},

		names:     make(map[string]etf.Pid),
		processes: make(map[etf.Pid]*Process),
	}
	go r.run()
	return &r
}

func (r *registrar) createNewPID(name string) etf.Pid {
	r.nextPID++
	return etf.Pid{
		Node:     etf.Atom(r.nodeName),
		Id:       r.nextPID,
		Serial:   1,
		Creation: byte(r.creation),
	}

}

func (r *registrar) run() {
	defer func() {
		close(r.channels.process)
		close(r.channels.unregisterProcess)
		close(r.channels.name)
		close(r.channels.unregisterName)
		close(r.channels.peer)
		close(r.channels.unregisterPeer)
		close(r.channels.reply)
	}()

	for {
		select {
		case p := <-r.channels.process:
			mailbox_size := DefaultProcessMailboxSize
			if size, ok := p.opts["mailbox-size"]; ok {
				mailbox_size = size.(int)
			}
			ctx, stop := context.WithCancel(r.node.context)
			pid := r.createNewPID(r.nodeName)
			wrapped_stop := func(reason string) {
				lib.Log("STOPPING: %#v with reason: %s", pid, reason)
				stop()
				r.UnregisterProcess(pid)
				r.node.monitor.ProcessTerminated(pid, reason)
			}
			process := &Process{
				local:   make(chan etf.Term, mailbox_size),
				remote:  make(chan etf.Tuple, mailbox_size),
				ready:   make(chan bool),
				self:    pid,
				context: ctx,
				Stop:    wrapped_stop,
				name:    p.name,
			}

			r.processes[process.self] = process
			if p.name != "" {
				r.names[p.name] = process.self
			}
			r.channels.reply <- process

			r.nextPID++

		case up := <-r.channels.unregisterProcess:
			if p, ok := r.processes[up]; ok {
				lib.Log("REGISTRAR unregistering process: %v", p.self)
				close(p.local)
				close(p.remote)
				close(p.ready)
				delete(r.processes, up)
				if (p.name) != "" {
					lib.Log("REGISTRAR unregistering name (%v): %s", p.self, p.name)
					delete(r.names, p.name)
				}
			}

		case n := <-r.channels.name:
			lib.Log("registering name %v", n)
			// TODO: implement it

		case un := <-r.channels.unregisterName:
			lib.Log("unregistering name %v", un)
			// TODO: implement it

		case p := <-r.channels.peer:
			lib.Log("registering peer %v", p)
			// TODO: implement it

		case up := <-r.channels.unregisterPeer:
			lib.Log("unregistering name %v", up)
			// TODO: implement it

		case <-r.node.context.Done():
			lib.Log("Finalizing registrar for %s (total number of processes: %d)", r.nodeName, len(r.processes))
			// FIXME: now its just call Stop function for
			// every single process. should we do that for the gen_servers
			// are running under supervisor?
			for _, p := range r.processes {
				lib.Log("FIN: %#v", p.name)
				p.Stop("normal")
			}
			return
		}
	}
}

func (r *registrar) RegisterProcess(object interface{}) Process {
	opts := map[string]interface{}{
		"mailbox-size": DefaultProcessMailboxSize, // size of channel for regular messages
	}
	return r.RegisterProcessExt("", object, opts)
}

func (r *registrar) RegisterProcessExt(name string, object interface{}, opts map[string]interface{}) Process {
	req := registerProcessRequest{
		name:   name,
		object: object,
		opts:   opts,
	}
	r.channels.process <- req

	select {
	case p := <-r.channels.reply:
		return *p
	}

}

// UnregisterProcess unregister process by Pid
func (r *registrar) UnregisterProcess(pid etf.Pid) {
	r.channels.unregisterProcess <- pid
}

// RegisterName register associates the name with pid
func (r *registrar) RegisterName(name string, pid etf.Pid) {
	req := registerNameRequest{name: name, pid: pid}
	r.channels.name <- req
}

// UnregisterName unregister named process
func (r *registrar) UnregisterName(name string) {
	r.channels.unregisterName <- name
}

func (r *registrar) RegisterPeer(name string, p peer) {
	req := registerPeer{name: name}
	r.channels.peer <- req
}

func (r *registrar) UnregisterPeer(name string) {
	r.channels.unregisterPeer <- name
}

// Registered returns a list of names which have been registered using Register
func (r *registrar) Registered() []Process {
	p := make([]Process, len(r.processes))
	i := 0
	for _, process := range r.processes {
		p[i] = *process
		i++
	}
	return p
}

// route incomming message to registered process
func (r *registrar) route(from etf.Pid, to etf.Term, message etf.Term) {
	// var toPid etf.Pid
	// switch tp := to.(type) {
	// case etf.Pid:
	// 	toPid = tp
	// case etf.Atom:
	// 	toPid, _ = n.registered[tp]
	// }
	// pcs := n.channels[toPid]
	// if from == nil {
	// 	lib.Log("SEND: To: %#v, Message: %#v", to, message)
	// 	pcs.in <- message
	// } else {
	// 	lib.Log("REG_SEND: (%#v )From: %#v, To: %#v, Message: %#v", pcs.inFrom, from, to, message)
	// 	pcs.inFrom <- etf.Tuple{from, message}
	// }
}
