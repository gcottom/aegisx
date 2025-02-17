package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gcottom/aegisx/dynamicroutes"
	"github.com/gcottom/aegisx/models"
	"github.com/gcottom/aegisx/util"
	"github.com/google/uuid"
)

type ExecuterService struct {
	GPTClient           *util.GPTClient
	Runtimes            sync.Map
	RetryLimit          int
	DynamicRouteService *dynamicroutes.DynamicRouteService
}

func CreatePrompt(prompt string, id string) string {
	log.Println("Creating prompt for base prompt: ", prompt)
	return `create a Go program that only uses functions from Go's standard library. 
The program should have an exported Shutdown function that takes no arguments and returns no values, the shutdown function should unbind the pattern and the port.
The program will be executed in a Yaegi interpreter running in restricted mode. 
Do not use syscall. The program should use fmt.Println and fmt.Printf to log significant events. 

💡 **IMPORTANT**: 
- The web server **must bind to a random available port**.
- **The program **MUST LOG THE PORT** in the format: 
  ` + "`PORT=<selected_port>`" + `**.
- The web server must handle HTTP requests and the handlers must be defined with http.NewServeMux function.
- **Form actions must use a base path of ` + "`/runtime/" + id + "` " + `**.
- **Handler functions must ****NOT**** include ` + "`/runtime/" + id + "` " + `**.
- **Handler functions MUST NOT INCLUDE BASEPATH**.

The program should do the following: ` + prompt
}

func CreateRebuildPrompt(prompt string, errorString string, code string) string {
	log.Println("Creating rebuild prompt for base prompt: ", prompt)
	return "fix the following go program that has the error: " + errorString + "\n\n" + code + "\n\n" + "which was built to satisfy the following prompt: " + prompt
}

func (s *ExecuterService) NewExecution(ctx context.Context, prompt string) (string, error) {
	log.Printf("New execution request for prompt: %s", prompt)
	runtimeID, err := s.PrepareRuntime(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("failed to prepare runtime: %w", err)
	}
	err = s.ExecuteRuntime(ctx, runtimeID)
	if err != nil {
		return "", fmt.Errorf("failed to execute runtime: %w", err)
	}
	return runtimeID, nil
}

func (s *ExecuterService) PrepareRuntime(ctx context.Context, prompt string) (string, error) {
	log.Printf("Preparing runtime for prompt: %s", prompt)
	id := strings.ReplaceAll(uuid.New().String(), "-", "")
	code, err := s.GPTClient.SendMessage(ctx, CreatePrompt(prompt, id))
	if err != nil {
		return "", fmt.Errorf("failed to get code from GPT: %w", err)
	}

	extractedCode := util.ExtractGoCode(code)

	interp, output := util.NewYaegiInterpreter()

	runtime := &models.Runtime{
		ID:           id,
		Prompt:       prompt,
		State:        "initializing",
		LastErrorMsg: "",
		RebuildCount: 0,
		Code:         extractedCode,
		CreatedAt:    time.Now(),
		Executer:     interp,
		Logs:         output,
	}

	s.Runtimes.Store(runtime.ID, runtime)
	return runtime.ID, nil
}

func (s *ExecuterService) ExecuteRuntime(ctx context.Context, runtimeID string) error {
	log.Printf("Executing runtime: %s", runtimeID)
	runtime, ok := s.Runtimes.Load(runtimeID)
	if !ok {
		return fmt.Errorf("runtime not found: %s", runtimeID)
	}
	runtimeData := runtime.(*models.Runtime)
	runtimeData.State = "running"
	ctx2, cancel := context.WithCancel(context.Background())
	runtimeData.StopFunction = cancel
	runtimeData.StartedAt = time.Now()
	s.Runtimes.Store(runtimeID, runtimeData)
	go func() {
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("runtime panicked: %v", r)
				log.Printf("Runtime panicked for executer with ID: %s err: %s", runtimeID, err)
			}
			if err != nil && err.Error() != "context canceled" {
				runtimeData.LastErrorMsg = err.Error()
				log.Printf("Runtime failed for executer with ID: %s err: %s", runtimeID, err)
				runtimeData.State = "error"
				s.Runtimes.Store(runtimeID, runtimeData)
				s.HandleRuntimeFailure(runtimeID)
			} else {
				log.Printf("Runtime finished successfully for executer with ID: %s", runtimeID)
				runtimeData.State = "finished"
				runtimeData.FinishedAt = time.Now()
				s.Runtimes.Store(runtimeID, runtimeData)
			}
			cancel()
		}()
		execDone := make(chan error, 1)
		go func() {
			isRegistered := false
			for {
				select {
				case <-execDone:
					return
				case <-ctx2.Done():
					return
				default:
					port := util.ExtractPort(runtimeData.Logs.String())
					if port > 0 && !isRegistered {
						runtimeData.Port = port
						runtimeData.Logs.Reset()
						log.Printf("Runtime started successfully for executer with ID: %s on port: %d", runtimeID, port)
						runtimeData.State = "running"
						s.Runtimes.Store(runtimeID, runtimeData)
						s.DynamicRouteService.RegisterReverseProxy(runtimeID, port)
						isRegistered = true
					} else {
						logData := runtimeData.Logs.String()
						logLines := strings.Split(logData, "\n")
						for _, line := range logLines {
							if line == "" {
								continue
							}
							log.Printf("executer ID: %s log: %s", runtimeID, line)
						}
						runtimeData.Logs.Reset()
						time.Sleep(1 * time.Second)

					}
				}
			}
		}()
		_, err = runtimeData.Executer.EvalWithContext(ctx2, runtimeData.Code)
		execDone <- err

	}()
	return nil
}

func (s *ExecuterService) StopRuntime(ctx context.Context, runtimeID string) error {
	runtime, ok := s.Runtimes.Load(runtimeID)
	if !ok {
		return fmt.Errorf("runtime not found: %s", runtimeID)
	}
	runtimeData := runtime.(*models.Runtime)
	if runtimeData.Executer != nil {
		_, _ = runtimeData.Executer.Eval("Shutdown()")
	}
	s.DynamicRouteService.DeregisterReverseProxy(runtimeID)
	time.Sleep(15 * time.Second)
	if runtimeData.StopFunction != nil {
		runtimeData.StopFunction()
	}
	runtimeData.State = "stopped"
	s.Runtimes.Store(runtimeID, runtimeData)
	return nil
}

func (s *ExecuterService) HandleRuntimeFailure(runtimeID string) error {
	log.Printf("Handling failure for runtime: %s", runtimeID)
	runtime, ok := s.Runtimes.Load(runtimeID)
	if !ok {
		return fmt.Errorf("runtime not found: %s", runtimeID)
	}
	runtimeData := runtime.(*models.Runtime)
	if runtimeData.RebuildCount >= s.RetryLimit {
		runtimeData.State = "failed"
		s.Runtimes.Store(runtimeID, runtimeData)
		return fmt.Errorf("runtime failed after %d attempts", s.RetryLimit)
	}
	runtimeData.RebuildCount++
	if runtimeData.Executer != nil {
		_, _ = runtimeData.Executer.Eval("Shutdown()")
	}
	s.DynamicRouteService.DeregisterReverseProxy(runtimeID)
	prompt := CreateRebuildPrompt(runtimeData.Prompt, runtimeData.LastErrorMsg, runtimeData.Code)
	code, err := s.GPTClient.SendMessage(context.Background(), prompt)
	if err != nil {
		return fmt.Errorf("failed to get code from GPT: %w", err)
	}
	interp, output := util.NewYaegiInterpreter()
	extractedCode := util.ExtractGoCode(code)
	runtimeData.Code = extractedCode
	runtimeData.State = "rebuilding"
	runtimeData.LastErrorMsg = ""
	runtimeData.Executer = interp
	runtimeData.Logs = output
	s.Runtimes.Store(runtimeID, runtimeData)
	return s.ExecuteRuntime(context.Background(), runtimeID)
}

func (s *ExecuterService) GetRuntimeStatus(ctx context.Context, runtimeID string) (*models.Runtime, error) {
	runtime, ok := s.Runtimes.Load(runtimeID)
	if !ok {
		return nil, fmt.Errorf("runtime not found: %s", runtimeID)
	}
	runtimeData := runtime.(*models.Runtime)
	return runtimeData, nil
}
