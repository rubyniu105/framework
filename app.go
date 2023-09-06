/* Copyright © INFINI Ltd. All rights reserved.
 * web: https://infinilabs.com
 * mail: hello#infini.ltd */

package framework

import (
	"context"
	"flag"
	"fmt"
	"github.com/shirou/gopsutil/v3/process"
	"infini.sh/framework/core/task"
	"infini.sh/framework/core/wrapper/taskset"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	log "github.com/cihub/seelog"
	"github.com/kardianos/service"
	"infini.sh/framework/core/config"
	"infini.sh/framework/core/daemon"
	"infini.sh/framework/core/env"
	"infini.sh/framework/core/errors"
	"infini.sh/framework/core/global"
	"infini.sh/framework/core/keystore"
	_ "infini.sh/framework/core/log"
	"infini.sh/framework/core/logger"
	"infini.sh/framework/core/module"
	"infini.sh/framework/core/pipeline"
	"infini.sh/framework/core/stats"
	"infini.sh/framework/core/util"
	"infini.sh/framework/lib/bytebufferpool"
	_ "infini.sh/framework/modules/queue"
	"infini.sh/license"
)

type App struct {
	environment    *env.Env
	numCPU         int
	maxMEM         int
	quitSignal     chan bool
	disableVerbose bool
	isDaemonMode   bool
	isDebug        bool
	pidFile        string
	configFile     string
	logLevel       string

	setup func()
	start func()
	stop  func()

	//for service
	svc     service.Service
	exit    chan os.Signal
	svcFlag string

	//atomic status
	state int32 //0 means running, 1 means stopping, 2 means stopped
}

const (
	env_SILENT_GREETINGS = "SILENT_GREETINGS"
	env_SERVICE_NAME     = "SERVICE_NAME"
)

func NewApp(name, desc, ver, buildNumber, commit, buildDate, eolDate, terminalHeader, terminalFooter string) *App {
	if terminalFooter == "" {
		terminalFooter = ("   __ _  __ ____ __ _  __ __     \n")
		terminalFooter += ("  / // |/ // __// // |/ // /    \n")
		terminalFooter += (" / // || // _/ / // || // /    \n")
		terminalFooter += ("/_//_/|_//_/  /_//_/|_//_/   \n\n")
		terminalFooter += ("©INFINI.LTD, All Rights Reserved.\n")
	}
	return &App{environment: env.NewEnv(name, desc, ver, buildNumber, commit, buildDate, eolDate, terminalHeader, terminalFooter)}
}

var debugFlagInitFunc func()
var debugInitFunc func()

func (app *App) Init(customFunc func()) {
	app.initWithFlags()
	app.initEnvironment(customFunc)

	if debugInitFunc != nil {
		debugInitFunc()
	}

	//init license
	license.Init()

	license.Verify()

	if app.environment.SystemConfig.ResourceLimit!=nil{
		//detect memory usage
		maxMemInBytes := uint64(app.environment.SystemConfig.ResourceLimit.Mem.MaxMemoryInBytes)
		if app.maxMEM > 0 {
			maxMemInBytes = uint64(app.maxMEM * 1024 * 1024)
		}

		if maxMemInBytes > 0 {
			checkPid := os.Getpid()
			p, _ := process.NewProcess(int32(checkPid))
			debug.SetMemoryLimit(int64(maxMemInBytes))

			var memoryInfoStat *process.MemoryInfoStat
			var err error
			//register memory OOM detector
			task1 := task.ScheduleTask{
				ID:          util.GetUUID(),
				Interval:    "10s",
				Description: "detect highly memory usage",
				Task: func(ctx context.Context) {
					memoryInfoStat, err = p.MemoryInfo()
					if err != nil {
						log.Error(err)
						return
					}
					if memoryInfoStat != nil {
						if memoryInfoStat.RSS > maxMemInBytes {
							log.Warnf("reached max memory limit! used: %v, limit:%v", util.ByteSize(memoryInfoStat.RSS), util.ByteSize(maxMemInBytes))
						}
					}
				}}
			task.RegisterScheduleTask(task1)
		}
	}

}

func (app *App) initWithFlags() {

	showversion := flag.Bool("v", false, "version")
	flag.StringVar(&app.logLevel, "log", "", "the log level, options: trace,debug,info,warn,error,off")
	flag.StringVar(&app.configFile, "config", app.environment.GetAppLowercaseName()+".yml", "the location of config file")

	//TODO bug fix
	//flag.BoolVar(&app.isDaemonMode, "daemon", false, "run in background as daemon")
	//flag.StringVar(&app.pidFile, "pidfile", "", "pidfile path (only for daemon mode)")

	flag.BoolVar(&app.isDebug, "debug", false, "run in debug mode, "+app.environment.GetAppName()+" will quit on panic immediately with full stack trace")
	flag.IntVar(&app.numCPU, "cpu", -1, "the number of CPUs to use")
	flag.IntVar(&app.maxMEM, "mem", -1, "the max size of Memory to use, soft limit in megabyte")
	flag.StringVar(&app.svcFlag, "service", "", "service management, options: install,uninstall,start,stop")

	if debugFlagInitFunc != nil {
		debugFlagInitFunc()
	}

	flag.Parse()

	app.environment.ISServiceMode = app.svcFlag != ""
	if *showversion {
		fmt.Println(app.environment.GetAppName(), app.environment.GetVersion(), app.environment.GetBuildNumber(), app.environment.GetBuildDate(), app.environment.GetEOLDate(), app.environment.GetLastCommitHash())
		os.Exit(1)
	}

	app.environment.IsDebug = app.isDebug
	if app.configFile != "" {
		path := util.TryGetFileAbsPath(app.configFile, true)
		if !util.FileExists(path) {
			fmt.Println(errors.Errorf("config file [%v] not exists", path))
			os.Exit(1)
		}
		app.environment.SetConfigFile(path)
	}
	err := app.environment.InitPaths(app.configFile)
	if err != nil {
		panic(err)
	}
	global.RegisterEnv(app.environment)

	if !util.FileExists(app.environment.GetDataDir()) {
		os.MkdirAll(app.environment.GetDataDir(), 0755)
	}
	if !util.FileExists(app.environment.GetLogDir()) {
		os.MkdirAll(app.environment.GetLogDir(), 0755)
	}
	if len(os.Args) > 1 && os.Args[1] == "keystore" {
		keystore.RunCmd(os.Args[2:])
	}
}

func (app *App) initEnvironment(customFunc func()) {
	ksResolver, err := keystore.GetVariableResolver()
	if err != nil {
		panic(err)
	}
	config.RegisterOption("keystore", ksResolver)
	err = task.RunWithContext("keystore", func(ctx context.Context) error {
		_, err = keystore.GetOrInitKeystore()
		if err != nil {
			log.Error(err)
		}
		keystore.Watch()
		return nil
	}, context.Background())
	if err != nil {
		panic(err)
	}
	global.RegisterShutdownCallback(keystore.CloseWatch)
	app.environment.Init()

	//allow use yml to configure the log level
	if app.logLevel != "" {
		app.environment.SystemConfig.LoggingConfig.LogLevel = app.logLevel
	}
	if app.environment.SystemConfig.LoggingConfig.IsDebug {
		app.environment.IsDebug = app.environment.SystemConfig.LoggingConfig.IsDebug
	}

	app.environment.CheckSetup()

	var (
		appName = app.environment.GetAppLowercaseName()
		baseDir = app.environment.GetLogDir()
	)
	logger.SetLogging(&app.environment.SystemConfig.LoggingConfig, appName, baseDir)

	if customFunc != nil {
		customFunc()
	}

	global.RegisterShutdownCallback(func() {
		config.StopWatchers()
	})
}

func (app *App) Setup(setup func(), start func(), stop func()) (allowContinue bool) {

	//skip on service mode
	if app.svcFlag != "" {
		return true
	}

	if app.environment.SystemConfig.ResourceLimit != nil {
		if app.environment.SystemConfig.ResourceLimit.CPU.MaxNumOfCPUs > 0 {
			app.numCPU = app.environment.SystemConfig.ResourceLimit.CPU.MaxNumOfCPUs
		}
	}

	if app.numCPU <= 0 {
		runtime.GOMAXPROCS(runtime.NumCPU())
	} else {
		runtime.GOMAXPROCS(app.numCPU)
	}

	//limit cpu
	if app.environment.SystemConfig.ResourceLimit!=nil&&app.environment.SystemConfig.ResourceLimit.CPU.CPUAffinityList!="" {
		taskset.SetCPUAffinityList(os.Getpid(),app.environment.SystemConfig.ResourceLimit.CPU.CPUAffinityList)
	}

	if _, ok := os.LookupEnv(env_SILENT_GREETINGS); !ok {
		fmt.Println(app.environment.GetWelcomeMessage())
	}

	log.Infof("initializing %s, pid: %v", app.environment.GetAppName(),os.Getpid())
	log.Infof("using config: %s", app.environment.GetConfigFile())

	//daemon
	if app.isDaemonMode {
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
			log.Trace(app.environment.GetAppName(), " enter daemon mode")
			runtime.LockOSThread()
			context := new(daemon.Context)
			if app.pidFile != "" {
				context.PidFileName = app.pidFile
				context.PidFilePerm = 0644
			}
			child, err := context.Reborn()
			if err != nil {
				panic(err)
			}

			if child != nil {
				fmt.Printf("[%s] started in background, pid: %v\n", app.environment.GetAppCapitalName(), os.Getpid()+1)
				return false
			}
			defer context.Release()

			runtime.UnlockOSThread()
		} else {
			fmt.Println("daemon mode only available on linux and darwin")
		}
	}

	//set path to persist id
	util.RestorePersistID(app.environment.GetDataDir())

	//loading plugins
	//plugins.Discovery(app.environment.GetPluginDir())

	if setup != nil {
		setup()
	}

	if start != nil {
		app.start = start
	}

	if stop != nil {
		app.stop = stop
	}

	return true
}

func (app *App) Shutdown() {
	//cleanup
	if !app.environment.SystemConfig.SkipInstanceDetect {
		util.ClearInstanceLock()
	}

	callbacks := global.ShutdownCallback()
	if callbacks != nil && len(callbacks) > 0 {
		for i, v := range callbacks {
			log.Trace("executing callback: ", i)
			v()
			log.Trace("executed callback: ", i)
		}
	}

	if r := recover(); r != nil {
		var v string
		switch r.(type) {
		case error:
			v = r.(error).Error()
		case runtime.Error:
			v = r.(runtime.Error).Error()
		case string:
			v = r.(string)
		}

		log.Error("panic: ", v)

		if global.Env().IsDebug {
			buf := make([]byte, 1<<20)
			runtime.Stack(buf, app.environment.IsDebug)
			fmt.Printf("\n%s\n", util.StripCtlAndExtFromUTF8(string(buf)))
		}
	}

	util.SnapshotPersistID()

	log.Flush()
	logger.Flush()

	if app.environment.IsDebug {
		stats, _ := stats.StatsMap()
		if stats != nil && len(stats) > 0 {
			fmt.Println(util.ToJson(stats, true))
		}
	}

	if !app.isDaemonMode && !app.disableVerbose {
		log.Infof("%s now terminated.", app.environment.GetAppName())
		log.Flush()
		//print goodbye message
		if _, ok := os.LookupEnv(env_SILENT_GREETINGS); !ok {
			fmt.Println(app.environment.GetGoodbyeMessage())
		}
	}
	os.Exit(0)
}

// for service
func (p *App) Start(s service.Service) error {

	if !p.environment.SystemConfig.SkipInstanceDetect {
		//check instance lock
		util.CheckInstanceLock(p.environment.GetDataDir())
	}

	p.quitSignal = make(chan bool)
	go p.run()
	return nil
}

func (p *App) run() error {

	//handle exit event
	p.exit = make(chan os.Signal, 1)
	signal.Notify(p.exit,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		os.Interrupt)

	var stopping bool
	var stopLock sync.Mutex
	go func() {
		stopLock.Lock()
		defer stopLock.Unlock()
		if stopping {
			return
		}

		defer func() {
			if !global.Env().IsDebug {
				if r := recover(); r != nil {
					var v string
					switch r.(type) {
					case error:
						v = r.(error).Error()
					case runtime.Error:
						v = r.(runtime.Error).Error()
					case string:
						v = r.(string)
					}
					log.Error("error on stopping modules,", v)
				}
			}
		}()

		s := <-p.exit
		if s == os.Interrupt || s.(os.Signal) == syscall.SIGINT || s.(os.Signal) == syscall.SIGTERM ||
			s.(os.Signal) == syscall.SIGKILL || s.(os.Signal) == syscall.SIGQUIT {
			stopping = true
			fmt.Printf("\n[%s] got signal: %v, start shutting down\n", p.environment.GetAppCapitalName(), s.String())

			//perform custom stop func first
			if p.stop != nil {
				p.stop()
			}

			atomic.StoreInt32(&p.state, 1)

			//wait modules to stop
			module.Stop()
			atomic.StoreInt32(&p.state, 2)
			p.quitSignal <- true
		}
	}()

	if p.start != nil {
		p.start()
	}

	global.RegisterBackgroundCallback(&global.BackgroundTask{Tag: "cleanup_bytes_buffer", Func: func() {
		bytebufferpool.CleanupIdleCachedBytesBuffer()
	}, Interval: 30 * time.Second})

	stats.RegisterStats("goroutine", pipeline.GetPoolStats)

	global.Register("APP_STATE", &p.state)

	//background job
	go func() {
		defer func() {
			if !global.Env().IsDebug {
				if r := recover(); r != nil {
					var v string
					switch r.(type) {
					case error:
						v = r.(error).Error()
					case runtime.Error:
						v = r.(runtime.Error).Error()
					case string:
						v = r.(string)
					}
					log.Error("error on running background jobs,", v)
				}
			}
		}()
		global.RunBackgroundCallbacks(&p.state)
	}()

	log.Infof("%s is up and running now.", p.environment.GetAppName())
	return nil
}

func (p *App) Stop(s service.Service) error {
	log.Trace("hit stop signal")
	p.exit <- os.Interrupt
	<-p.quitSignal
	log.Trace("stopped")
	return nil
}

func (app *App) Run() {
	var err error

	//init service
	svcOptions := make(service.KeyValue)
	svcOptions["Restart"] = "on-success"
	svcOptions["SuccessExitStatus"] = "1 2 8 SIGKILL"
	svcOptions["LimitNOFILE"] = 1024000

	workdir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	serviceName := app.environment.GetAppLowercaseName()
	if v, ok := os.LookupEnv(env_SERVICE_NAME); ok {
		serviceName = util.TrimSpaces(v)
		if global.Env().IsDebug {
			log.Debug("customized service name: ", serviceName)
		}
	}

	svcConfig := &service.Config{
		Name:             serviceName,
		DisplayName:      app.environment.GetAppName(),
		Description:      app.environment.GetAppDesc(),
		WorkingDirectory: workdir,
		//Dependencies: []string{
		//	"Requires=network.target",
		//	"After=network-online.target syslog.target"},
		Option: svcOptions,
	}

	app.svc, err = service.New(app, svcConfig)
	if err != nil {
		panic(err)
	}

	if len(app.svcFlag) != 0 {
		app.disableVerbose = true
		err = service.Control(app.svc, app.svcFlag)
		if err != nil {
			panic(err)
		}
		fmt.Println("Success")
		return
	}

	err = (app.svc).Run()
	if err != nil {
		log.Error(err)
	}
}
