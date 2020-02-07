package ergonode

// http://erlang.org/doc/apps/kernel/application.html

import (
	"fmt"
	"time"

	"github.com/halturin/ergonode/etf"
	"github.com/halturin/ergonode/lib"
)

type ApplicationStrategy = string

const (
	// Restart types:

	// ApplicationStrategyPermanent If a permanent application terminates,
	// all other applications and the runtime system (node) are also terminated.
	ApplicationStrategyPermanent = "permanent"

	// ApplicationStrategyTemporary If a temporary application terminates,
	// this is reported but no other applications are terminated.
	ApplicationStrategyTemporary = "temporary"

	// ApplicationStrategyTransient If a transient application terminates
	// with reason normal, this is reported but no other applications are
	// terminated. If a transient application terminates abnormally, that
	// is with any other reason than normal, all other applications and
	// the runtime system (node) are also terminated.
	ApplicationStrategyTransient = "transient"
)

// ApplicationBehavior interface
type ApplicationBehavior interface {
	Load(args ...interface{}) (ApplicationSpec, error)
	Start(process *Process, args ...interface{})
}

type ApplicationSpec struct {
	Name         string
	Description  string
	Version      string
	MaxTime      time.Duration
	Applications []string
	Environment  map[string]interface{}
	// Depends		[]
	Children []ApplicationChildSpec
	Strategy ApplicationStrategy
	app      ApplicationBehavior
	process  *Process
}

type ApplicationChildSpec struct {
	Child   interface{}
	Args    []interface{}
	process *Process
}

// Application is implementation of ProcessBehavior interface
type Application struct{}

type ApplicationInfo struct {
	Name        string
	Description string
	Version     string
}

func (a *Application) loop(p *Process, object interface{}, args ...interface{}) string {
	spec := args[0].(ApplicationSpec)

	if spec.Environment != nil {
		for k, v := range spec.Environment {
			p.SetEnv(k, v)
		}
	}

	object.(ApplicationBehavior).Start(p, args[1:]...)
	lib.Log("Application spec %#v\n", spec)
	p.ready <- true

	if spec.MaxTime == 0 {
		spec.MaxTime = time.Second * 31536000 * 100 // let's define default lifespan 100 years :)
	}

	if !a.startChildren(p, spec.Children[:]) {
		a.stopChildren(p.Self(), spec.Children[:], "failed")
		return "failed"
	}

	// to prevent of timer leaks due to its not GCed until the timer fires
	timer := time.NewTimer(spec.MaxTime)
	defer timer.Stop()

	for {
		select {
		case ex := <-p.gracefulExit:
			a.stopChildren(ex.from, spec.Children, string(ex.reason))
			return ex.reason

		case <-p.Context.Done():
			// node is down or killed using p.Kill()
			return "kill"
		case <-timer.C:
			// time to die
			p.Exit(p.Self(), "normal")
		case msg := <-p.mailBox:
			if len(msg) == 0 {
				continue // ignore
			}
			switch r := msg[0].(type) {
			case etf.Tuple:
				var terminatedProcess *Process
				// waiting for {'EXIT', Pid, Reason}
				if len(r) != 3 || r.Element(1) != etf.Atom("EXIT") {
					// unknown. ignoring
					continue
				}
				terminated := r.Element(2).(etf.Pid)
				reason := r.Element(3).(etf.Atom)

				for i := range spec.Children {
					child := spec.Children[i].process
					if child != nil && child.Self() == terminated {
						terminatedProcess = child
						break
					}
				}

				switch spec.Strategy {
				case ApplicationStrategyPermanent:
					a.stopChildren(terminated, spec.Children, string(reason))
					fmt.Printf("Application (process) %s stopped with reason %s (permanent)", terminatedProcess.Name(), reason)
					p.Node.Stop()
					return "shutdown"

				case ApplicationStrategyTransient:
					if reason == etf.Atom("normal") || reason == etf.Atom("shutdown") {
						fmt.Printf("Application (process) %s stopped with reason %s (transient)", terminatedProcess.Name(), reason)
						continue
					}
					a.stopChildren(terminated, spec.Children, "normal")
					fmt.Printf("Application (process) %s stopped with reason %s. Node %s is shutting down",
						terminatedProcess.Name(), reason, p.Node.FullName)
					p.Node.Stop()
					return string(reason)

				case ApplicationStrategyTemporary:
					fmt.Printf("Application (process) %s stopped with reason %s (temporary)", terminatedProcess.Name(), reason)
				}

			}
		}

	}
}
func (a *Application) stopChildren(from etf.Pid, children []ApplicationChildSpec, reason string) {
	for i := range children {
		child := children[i].process
		if child != nil && child.self != from {
			children[i].process.Exit(from, reason)
		}
	}
}

func (a *Application) startChildren(parent *Process, children []ApplicationChildSpec) bool {
	for i := range children {
		p := startChild(parent, "", children[i].Child, children[i].Args...)
		if p == nil {
			return false
		}
		children[i].process = p
	}
	return true
}
