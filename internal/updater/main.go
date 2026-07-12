package updater

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	appName     = "PatchBoard"
	appDirName  = "PatchBoard"
	defaultHost = "127.0.0.1"
	defaultPort = 4183

	apiReadTimeout = 30 * time.Second

	flagHelp            = "--help"
	flagNoBrowser       = "--no-browser"
	flagPort            = "--port"
	flagToken           = "--token"
	flagTask            = "--task"
	flagElevatedWorker  = "--elevated-worker"
	flagSelfUpdateApply = "--self-update-apply"
	flagUninstall       = "--uninstall"
	flagUninstallApply  = "--uninstall-apply"
	flagNoElevate       = "--no-elevate"
)

var cryptoRandomRead = rand.Read

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := cryptoRandomRead(b); err != nil {
		return "", fmt.Errorf("generate secure token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

type cliMode string

const (
	cliModeServer          cliMode = "server"
	cliModeHelp            cliMode = "help"
	cliModeAutoUpdate      cliMode = "auto-update"
	cliModeElevatedWorker  cliMode = "elevated-worker"
	cliModeStoreInventory  cliMode = "store-inventory-worker"
	cliModeStoreDiscovery  cliMode = "store-update-discovery-worker"
	cliModeSelfUpdateApply cliMode = "self-update-apply"
	cliModeUninstall       cliMode = "uninstall"
	cliModeUninstallApply  cliMode = "uninstall-apply"
)

type cliOptions struct {
	Mode                cliMode
	NoBrowser           bool
	Token               string
	Port                int
	PortSet             bool
	SelfUpdateTarget    string
	SelfUpdateParentPID int
	SelfUpdateSHA256    string
	SelfUpdateRestart   bool
	SelfUpdateElevated  bool
	SelfUpdateManifest  string
	UninstallTarget     string
	UninstallParentPID  int
}

type trayController interface {
	Stop()
}

type serverHooks struct {
	startTray func(*App, string) (trayController, error)
	openURL   func(string) error
}

var productionServerHooks = serverHooks{
	startTray: func(app *App, url string) (trayController, error) {
		return startTray(app, url)
	},
	openURL: openURL,
}

func parseCLI(args []string) (cliOptions, error) {
	options := cliOptions{Mode: cliModeServer}
	set := flag.NewFlagSet("PatchBoard", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	help := set.Bool("help", false, "")
	set.BoolVar(help, "h", false, "")
	noBrowser := set.Bool(strings.TrimPrefix(flagNoBrowser, "--"), false, "")
	token := set.String(strings.TrimPrefix(flagToken, "--"), "", "")
	port := set.Int(strings.TrimPrefix(flagPort, "--"), 0, "")
	task := set.String(strings.TrimPrefix(flagTask, "--"), "", "")
	elevatedWorker := set.Bool(strings.TrimPrefix(flagElevatedWorker, "--"), false, "")
	storeInventoryWorker := set.Bool(strings.TrimPrefix(storeInventoryWorkerFlag, "--"), false, "")
	storeUpdateDiscoveryWorker := set.Bool(strings.TrimPrefix(storeUpdateDiscoveryWorkerFlag, "--"), false, "")
	selfUpdateApply := set.Bool(strings.TrimPrefix(flagSelfUpdateApply, "--"), false, "")
	selfUpdateTarget := set.String("self-update-target", "", "")
	selfUpdateParentPID := set.String("self-update-parent-pid", "", "")
	selfUpdateSHA256 := set.String("self-update-sha256", "", "")
	selfUpdateRestart := set.Bool("self-update-restart", false, "")
	selfUpdateElevated := set.Bool("self-update-elevated", false, "")
	selfUpdateManifest := set.String("self-update-manifest", "", "")
	uninstall := set.Bool(strings.TrimPrefix(flagUninstall, "--"), false, "")
	uninstallApply := set.Bool(strings.TrimPrefix(flagUninstallApply, "--"), false, "")
	uninstallTarget := set.String("uninstall-target", "", "")
	uninstallParentPID := set.String("uninstall-parent-pid", "", "")
	workerPipe := set.String("worker-pipe", "", "")
	workerCapability := set.String("worker-capability", "", "")
	workerUserSID := set.String("worker-user-sid", "", "")
	workerSessionID := set.String("worker-session-id", "", "")
	noElevate := set.Bool(strings.TrimPrefix(flagNoElevate, "--"), false, "")
	if err := set.Parse(args); err != nil {
		return options, err
	}
	if *noElevate {
		return options, fmt.Errorf("%s is not supported; the WebUI starts asInvoker and elevates only individual privileged actions", flagNoElevate)
	}
	if *help {
		options.Mode = cliModeHelp
		return options, nil
	}
	workerProtocolFlagSet := strings.TrimSpace(*workerPipe) != "" ||
		strings.TrimSpace(*workerCapability) != "" ||
		strings.TrimSpace(*workerUserSID) != "" ||
		strings.TrimSpace(*workerSessionID) != ""
	if err := validateWorkerProtocolFlags(
		workerProtocolFlagSet,
		*elevatedWorker,
	); err != nil {
		return options, err
	}
	if setWorkerMode(&options, *storeInventoryWorker, *storeUpdateDiscoveryWorker, *elevatedWorker) {
		return options, nil
	}
	if *selfUpdateApply {
		if err := applySelfUpdateCLIOptions(
			&options,
			*selfUpdateTarget,
			*selfUpdateParentPID,
			*selfUpdateSHA256,
			*selfUpdateRestart,
			*selfUpdateElevated,
			*selfUpdateManifest,
		); err != nil {
			return options, err
		}
		return options, nil
	}
	if *uninstall && *uninstallApply {
		return options, errors.New("--uninstall and --uninstall-apply cannot be combined")
	}
	if *uninstallApply {
		if err := applyUninstallCLIOptions(&options, *uninstallTarget, *uninstallParentPID); err != nil {
			return options, err
		}
		return options, nil
	}
	if *uninstall {
		options.Mode = cliModeUninstall
		return options, nil
	}
	if applyTaskCLIOptions(&options, *task) {
		return options, nil
	}
	if strings.TrimSpace(*task) != "" {
		return options, fmt.Errorf("unsupported task %q", *task)
	}
	return applyServerCLIOptions(&options, *noBrowser, *token, *port)
}

func validateWorkerProtocolFlags(protocolFlagsSet, elevatedWorker bool) error {
	if protocolFlagsSet && !elevatedWorker {
		return errors.New("worker protocol flags require --elevated-worker")
	}
	return nil
}

func setWorkerMode(options *cliOptions, storeInventoryWorker, storeUpdateDiscoveryWorker, elevatedWorker bool) bool {
	switch {
	case storeInventoryWorker:
		options.Mode = cliModeStoreInventory
	case storeUpdateDiscoveryWorker:
		options.Mode = cliModeStoreDiscovery
	case elevatedWorker:
		options.Mode = cliModeElevatedWorker
	default:
		return false
	}
	return true
}

func applySelfUpdateCLIOptions(options *cliOptions, target, parentPIDText, sha256Text string, restart, elevated bool, manifestPath string) error {
	manifestPath = strings.TrimSpace(manifestPath)
	if manifestPath != "" {
		if strings.TrimSpace(target) != "" || strings.TrimSpace(parentPIDText) != "" || strings.TrimSpace(sha256Text) != "" || restart || elevated {
			return errors.New("self-update manifest mode cannot be combined with legacy apply arguments")
		}
		options.Mode = cliModeSelfUpdateApply
		options.SelfUpdateManifest = manifestPath
		return nil
	}
	parentPID, err := parseSelfUpdateParentPID(parentPIDText)
	if err != nil {
		return err
	}
	options.Mode = cliModeSelfUpdateApply
	options.SelfUpdateTarget = strings.TrimSpace(target)
	options.SelfUpdateParentPID = parentPID
	options.SelfUpdateSHA256 = strings.TrimSpace(sha256Text)
	options.SelfUpdateRestart = restart
	options.SelfUpdateElevated = elevated
	return nil
}

func applyUninstallCLIOptions(options *cliOptions, target, parentPIDText string) error {
	parentPID, err := parseSelfUpdateParentPID(parentPIDText)
	if err != nil {
		return err
	}
	options.Mode = cliModeUninstallApply
	options.UninstallTarget = strings.TrimSpace(target)
	options.UninstallParentPID = parentPID
	return nil
}

func applyTaskCLIOptions(options *cliOptions, task string) bool {
	if !strings.EqualFold(strings.TrimSpace(task), "auto-update") {
		return false
	}
	options.Mode = cliModeAutoUpdate
	return true
}

func applyServerCLIOptions(options *cliOptions, noBrowser bool, token string, port int) (cliOptions, error) {
	options.NoBrowser = noBrowser
	options.Token = strings.TrimSpace(token)
	if port == 0 {
		return *options, nil
	}
	if port < 1 || port > 65535 {
		return *options, fmt.Errorf("port must be between 1 and 65535")
	}
	options.Port = port
	options.PortSet = true
	return *options, nil
}

func helpText() string {
	return strings.TrimSpace(`PatchBoard

Usage:
  PatchBoard.exe [--no-browser] [--port PORT] [--token TOKEN]
  PatchBoard.exe --task auto-update
  PatchBoard.exe --uninstall

Options:
  --no-browser   Start the local WebUI without opening a browser. Prints the URL.
  --port PORT    Bind the WebUI to this local TCP port. Fails if unavailable.
  --token TOKEN  Use a caller-provided bootstrap token instead of generating one.
  --help, -h     Show this help.

Internal unsupported modes:
  --elevated-worker, --store-inventory-worker, --store-update-discovery-worker,
  --self-update-apply, and --uninstall-apply are implementation details for
  privileged package actions, isolated current-user Store inventory/discovery,
  verified application self-replacement, and installed-copy cleanup.`) + "\n"
}

func listenerPort(listener net.Listener) int {
	if listener == nil {
		return 0
	}
	if tcp, ok := listener.Addr().(*net.TCPAddr); ok {
		return tcp.Port
	}
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(portText)
	return port
}

func listenForServer(host string, requestedPort int, explicit bool) (net.Listener, error) {
	if explicit {
		listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(requestedPort)))
		if err != nil {
			return nil, fmt.Errorf("bind %s:%d: %w", host, requestedPort, err)
		}
		return listener, nil
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(requestedPort)))
	if err == nil {
		return listener, nil
	}
	listener, fallbackErr := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if fallbackErr != nil {
		return nil, fmt.Errorf("bind %s:%d failed (%v), and OS-chosen fallback failed: %w", host, requestedPort, err, fallbackErr)
	}
	return listener, nil
}

func serverURL(host string, port int, token string) string {
	return fmt.Sprintf("http://%s/?token=%s", net.JoinHostPort(host, strconv.Itoa(port)), token)
}

func runServerWithOptions(options cliOptions, hooks serverHooks) error {
	token := options.Token
	if token == "" {
		token = os.Getenv("UPDATER_TOKEN")
	}
	if token == "" {
		generated, err := randomToken()
		if err != nil {
			return err
		}
		token = generated
	}
	port := options.Port
	portSet := options.PortSet
	if !portSet {
		if override := os.Getenv("UPDATER_PORT"); override != "" {
			parsed, err := strconv.Atoi(override)
			if err != nil || parsed < 1 || parsed > 65535 {
				return fmt.Errorf("invalid UPDATER_PORT %q", override)
			}
			port = parsed
			portSet = true
		}
	}
	if port == 0 {
		port = defaultPort
	}
	listener, err := listenForServer(defaultHost, port, portSet)
	if err != nil {
		return err
	}
	defer listener.Close()
	actualPort := listenerPort(listener)
	sessionToken, err := randomToken()
	if err != nil {
		return err
	}
	checker := defaultGitHubReleaseChecker()
	app := &App{
		webSession: webSession{
			bootstrapToken: token,
			sessionToken:   sessionToken,
			listenHost:     defaultHost,
			listenPort:     actualPort,
		},
		inventoryService: inventoryService{storeBackgroundScanEnabled: true},
		appUpdateChecker: checker,
	}
	defer func() {
		app.beginShutdown()
		if !app.waitForBackgroundWork(gracefulShutdownTimeout) {
			appLog("Server exit timed out waiting for background work.")
		}
		app.runShutdownCleanups()
	}()
	stopSignalWatcher := app.startShutdownSignalWatcher()
	defer stopSignalWatcher()

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.serveHTTP)
	server := newLocalHTTPServer(listener.Addr().String(), mux)
	app.server = server
	app.refreshStatus(true)
	app.refreshInventory(true)

	url := serverURL(defaultHost, actualPort, token)
	appLog("Server listening on http://%s.", net.JoinHostPort(defaultHost, strconv.Itoa(actualPort)))
	if hooks.startTray != nil {
		tray, err := hooks.startTray(app, url)
		if err != nil {
			appLog("Tray icon could not be started: %s", err)
		} else {
			app.addShutdownCleanup(tray.Stop)
		}
	}
	if !options.NoBrowser {
		if hooks.openURL != nil {
			_ = hooks.openURL(url)
		}
	} else {
		fmt.Println(url)
	}
	return server.Serve(listener)
}

func newLocalHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       apiReadTimeout,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
}

func (app *App) startShutdownSignalWatcher() func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	stopWatcher := app.watchShutdownSignals(signals)
	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			signal.Stop(signals)
			stopWatcher()
		})
	}
}

func (app *App) watchShutdownSignals(signals <-chan os.Signal) func() {
	done := make(chan struct{})
	var stopOnce sync.Once
	go func() {
		select {
		case <-signals:
			app.requestShutdown("OS signal")
		case <-done:
		}
	}()
	return func() {
		stopOnce.Do(func() {
			close(done)
		})
	}
}

func argValue(name string) (string, bool) {
	prefix := name + "="
	for i, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix), true
		}
		if arg == name && i+2 < len(os.Args) {
			return os.Args[i+2], true
		}
	}
	return "", false
}

func Main() {
	enableDPIAwareness()

	options, err := parseCLI(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	configureProcessExecutionMode(options.Mode)
	switch options.Mode {
	case cliModeHelp:
		fmt.Print(helpText())
		return
	case cliModeStoreInventory:
		os.Exit(runStoreInventoryWorkerFromArgs())
	case cliModeStoreDiscovery:
		os.Exit(runStoreUpdateDiscoveryWorkerFromArgs())
	case cliModeSelfUpdateApply:
		if err := runSelfUpdateApplyFromOptions(options); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	case cliModeUninstall:
		if err := runApplicationUninstall(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	case cliModeUninstallApply:
		if err := runApplicationUninstallApply(options.UninstallTarget, options.UninstallParentPID); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	case cliModeElevatedWorker:
		if err := runElevatedWorkerFromArgs(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return
	case cliModeAutoUpdate:
		results := runAutoUpdate()
		data, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(data))
		return
	}

	if err := runServerWithOptions(options, productionServerHooks); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
	}
}
