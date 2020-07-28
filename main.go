package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

type config struct {
	addr      string
	port      string
	sleepTime int
}

type command int

const (
	stop command = iota
	restart
	start
)

func main() {
	conf := flags()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: parent-trap [args]")
		os.Exit(1)
	}

	bin, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to find command:", args[0], err)
		os.Exit(1)
	}
	cmd := mkCommand(bin, args)
	fmt.Fprintln(os.Stderr, "Running", args)
	err = cmd.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to start command:", args[0], err)
	}

	// gotta call wait for ProcessState to be populated after exit
	cmdDone := make(chan struct{}, 1)
	go func(cmd *exec.Cmd) {
		cmd.Wait()
		cmdDone <- struct{}{}
	}(cmd)

	commandChan := make(chan command)
	fmt.Println("starting server")
	svr(conf, commandChan, time.Duration(conf.sleepTime))

	fmt.Println("Waiting for commands")
	go func(cmd *exec.Cmd, commandChan chan command, cmdDone chan struct{}, sleepTime time.Duration) {
		for {
			select {
			case c := <-commandChan:
				switch c {
				case start:
					fmt.Println("starting process")
					cmd = mkCommand(bin, args)
					err = cmd.Start()
					if err != nil {
						fmt.Fprintln(os.Stderr, "failed to start process:", bin)
						os.Exit(1)
					}
					// we assume that it got written
					go func(cmd *exec.Cmd) {
						cmd.Wait()
						cmdDone <- struct{}{}
					}(cmd)
				case stop:
					fmt.Println("stopping process")
					pid := cmd.Process.Pid
					fmt.Println("sending interrupt")
					err = cmd.Process.Signal(os.Interrupt)
					if err != nil {
						fmt.Println("Failed to signal process", pid)
					}
					time.Sleep(time.Second * sleepTime)
					select {
					case _ = <-cmdDone:
						fmt.Println("Process stopped")
					default:
					}
					cmd.Process.Kill()
				case restart:
					fmt.Println("stopping process")
					pid := cmd.Process.Pid
					fmt.Println("sending interrupt")
					err = cmd.Process.Signal(os.Interrupt)
					if err != nil {
						fmt.Println("Failed to signal process", pid)
					}
					fmt.Println("sleeping")
					time.Sleep(sleepTime * time.Second)
					select {
					case _ = <-cmdDone:
						fmt.Println("command should be done")
					default:
						fmt.Println("killing process")
						cmd.Process.Kill()
					}
					fmt.Println("There should be a process state")
					if !cmd.ProcessState.Exited() {
						fmt.Println("I give up")
						os.Exit(1)
					}
					fmt.Println("starting process")
					cmd = mkCommand(bin, args)
					err = cmd.Start()
					if err != nil {
						fmt.Fprintln(os.Stderr, "failed to start process:", bin)
						os.Exit(1)
					}
					// we assume that it got written
					go func(cmd *exec.Cmd) {
						cmd.Wait()
						cmdDone <- struct{}{}
					}(cmd)
				}
			default:
				select {
				case _ = <-cmdDone:
					fmt.Println("process restarted or exited unexpectedly")
					os.Exit(1)
				default:
				}
				time.Sleep(time.Second * 1)
			}
		}
	}(cmd, commandChan, cmdDone, time.Duration(conf.sleepTime))

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	for {
		sig := <-signals
		fmt.Println(sig)
		commandChan <- stop
		time.Sleep(time.Second * 2)
		os.Exit(0)
	}

}

func flags() config {
	addr := flag.String("http-addr", "127.0.0.1", "Address to bind on for HTTP Server")
	port := flag.String("http-port", "6060", "Port to bind on for HTTP Server")
	sleepTime := flag.Int("sleep-time", 3, "sleep time in seconds after triggering an action")
	flag.Parse()
	return config{
		addr:      *addr,
		port:      *port,
		sleepTime: *sleepTime,
	}
}

func svr(c config, ch chan<- command, sleepTime time.Duration) {
	go func() {
		log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%s", c.addr, c.port), http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/restart":
					ch <- restart
					time.Sleep(sleepTime * time.Second)
				case "/stop":
					fmt.Println("sending stop")
					ch <- stop
					time.Sleep(sleepTime * time.Second)
				case "/start":
					ch <- start
					time.Sleep(sleepTime * time.Second)
				case "/exit":
					ch <- stop
					time.Sleep(sleepTime * time.Second)
					os.Exit(0)
				default:
					fmt.Fprintln(os.Stderr, "unknown command", r.URL.Path)
				}
				w.WriteHeader(200)
			},
		)))
	}()
}

func mkCommand(bin string, args []string) *exec.Cmd {
	return &exec.Cmd{
		Path:   bin,
		Args:   args,
		Env:    os.Environ(),
		Dir:    "",
		Stdin:  nil,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

func sleep(d time.Duration) string {
	time.Sleep(d)
	return "I'm up"
}
