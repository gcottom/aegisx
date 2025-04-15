package executer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gcottom/aegisx/config"
	"github.com/gcottom/aegisx/models"
	"github.com/gcottom/aegisx/routes"
	"github.com/gcottom/aegisx/util"
	"github.com/gcottom/aegisx/validators/code"
	"github.com/google/uuid"
)

type ExecuterService struct {
	GPTClient           *util.GPTClient
	Runtimes            sync.Map
	RetryLimit          int
	DynamicRouteService *routes.DynamicRouteService
	Config              *config.Config
	ActiveRetries       sync.Map // Track active retries by runtimeID
}

func CreateTitlePrompt(prompt string) string {
	log.Println("Creating title prompt for base prompt:", prompt)
	return `You are a concise title generator for Go programs.  
Your task is to generate a **short, clear title** based on a program prompt.  

**Title Rules:**  
✅ Titles should be **2 to 5 words** maximum.  
✅ Use **Title Case** (capitalize major words).  
✅ **No punctuation**, unless it is a recognized part of a name (e.g., OAuth, JWT).  
✅ Focus on the **core functionality** or **primary feature**.  
✅ Use **nouns** or **noun phrases**.  

**Examples:**  
- 🛡️ JWT Decoder  
- 📦 Inventory Manager  
- 📝 To-Do List  
- 📊 Stock Tracker  
- 🌐 Web Server Generator  
- 📅 Appointment Scheduler  

**Output Format:**  
Return only the title—no extra commentary.  
Prompt: ` + prompt
}

func CreatePrompt(prompt string, id string) string {
	log.Println("Creating prompt for base prompt:", prompt)
	base := `You are a Go expert. Generate a Go program that meets the following requirements:
🛡️ Core Requirements:
✅ Single Page Application (SPA) with a web server.
✅ The application should have persistent state and storage management.
✅ Export an Shutdown() function with no arguments and no return values.
✅ Shutdown() must stop the server and release the port.
🚫 Do NOT use any global variables.
🚫 Do NOT use syscall.
✅ Use only fmt and net/http for logs and server operations.
📊 Logging Rules:
✅ Use fmt.Println() or fmt.Printf() for logs.
✅ Log the assigned port as: \"PORT=<selected_port>\"
🌐 Web Server Requirements:
✅ Bind to a random available port.
✅ Use http.NewServeMux for all routes.
✅ ****HTML Form Rule: All HTML form actions must use /runtime/` + id + `/.... ****
✅ Correct Handler Example:
mux := http.NewServeMux()
mux.HandleFunc("/hello", helloHandler) // ✅ Correct

🚫 Incorrect Handler Example:
mux.HandleFunc("/runtime/` + id + `/hello", helloHandler) // ❌ Wrong
*******Do NOT use the /runtime/` + id + `/ prefix in the handler registration.********
		
💡 Program Instructions:
Third party packages are permitted, but they must be stable and well-known.
Return only the source code—no additional commentary.
The program must compile and run as provided.
The program must be a complete, runnable Go program.
The front end must be able to fully interact with the backend.
Animation and css/javascript are permitted
Implement the above based on the user prompt:
`
	if strings.Contains(prompt, base) {
		return prompt
	} else {
		return base + prompt
	}

}

func CreateRebuildPrompt(prompt string, errorString string, code string) string {
	log.Println("Creating rebuild prompt due to error: ", errorString)
	return `You are a Go expert. 
The following program was generated based on a user prompt but has an error. 
Please correct the error while adhering to the original prompt and best practices. 

💥 ERROR:
` + errorString + `

📝 ORIGINAL CODE:
` + code + `

📝 ORIGINAL PROMPT:
` + prompt + `

✅ REQUIREMENTS:
- The program must compile and run as provided.
- Use http.NewServeMux and bind to a random port.
- Ensure 'PORT=<port>' is logged.
- Return only the corrected Go program.
`
}

// waitForPassedHealthCheck polls until the runtime's PassedHealthCheck is true,
// or the context is canceled or the runtime enters an error/failed state.
func waitForPassedHealthCheck(ctx context.Context, s *ExecuterService, runtimeID string) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled for runtime %s", runtimeID)
		case <-ticker.C:
			runtime, err := s.GetRuntime(ctx, runtimeID)
			if err != nil {
				return err
			}
			if runtime.PassedHealthCheck {
				return nil
			}
			if runtime.State == "error" || runtime.State == "failed" {
				return fmt.Errorf("runtime %s entered error state", runtimeID)
			}
		}
	}
}

// NewConcurrentExecution spawns 3 concurrent attempts, each with its own context.
// It returns the runtimeID of the first execution that passes its health check.
func (s *ExecuterService) NewConcurrentExecution(ctx context.Context, prompt string) (string, error) {
	type result struct {
		runtimeID string
		err       error
	}
	concurrency := 5
	results := make(chan result, concurrency)
	// Keep a slice of cancel functions for each goroutine.
	var cancels []context.CancelFunc
	var runtimes []string

	for i := 0; i < concurrency; i++ {
		// Create a new independent context for each execution.
		newCtx, cancel := context.WithCancel(ctx)
		cancels = append(cancels, cancel)

		go func(ctx context.Context) {
			// Create a new runtime.
			runtimeID, err := s.NewExecution(ctx, prompt)
			if err != nil {
				results <- result{"", err}
				return
			}
			runtimes = append(runtimes, runtimeID)
			// Wait until the runtime reports that it passed the health check.
			if err := waitForPassedHealthCheck(ctx, s, runtimeID); err != nil {
				results <- result{"", err}
				return
			}
			results <- result{runtimeID, nil}
		}(newCtx)
	}

	var finalErr error
	// Wait for all three results.
	for i := 0; i < concurrency; i++ {
		res := <-results
		if res.err == nil {
			// Cancel all other contexts if one execution passes its health check.
			for _, cancel := range cancels {
				cancel()
			}
			runtimes = util.RemoveItem(runtimes, res.runtimeID)
			for _, runtimeID := range runtimes {
				s.StopRuntime(ctx, runtimeID)
				s.DynamicRouteService.DeregisterReverseProxy(runtimeID)
			}
			runtimeData, ok := s.Runtimes.Load(res.runtimeID)
			if !ok {
				return "", fmt.Errorf("runtime not found: %s", res.runtimeID)
			}
			runtime := runtimeData.(*models.Runtime)
			title, err := s.GPTClient.SendMessage(ctx, CreateTitlePrompt(runtime.Prompt))
			if err != nil {
				return "", fmt.Errorf("failed to get title from GPT: %w", err)
			}
			runtime.Title = title
			return res.runtimeID, nil
		}

		finalErr = res.err
	}
	return "", fmt.Errorf("all concurrent execution attempts failed, last error: %w", finalErr)
}

func (s *ExecuterService) NewExecution(ctx context.Context, prompt string) (string, error) {
	log.Printf("New execution request for prompt: %s", prompt)
	runtimeID, err := s.PrepareRuntime(ctx, prompt, "")
	if err != nil {
		return "", fmt.Errorf("failed to prepare runtime: %w", err)
	}
	err = s.ExecuteRuntime(ctx, runtimeID)
	if err != nil {
		return "", fmt.Errorf("failed to execute runtime: %w", err)
	}
	return runtimeID, nil
}

func (s *ExecuterService) PrepareRuntime(ctx context.Context, prompt string, id string) (string, error) {
	log.Printf("Preparing runtime for prompt: %s", prompt)
	if id == "" {
		id = strings.ReplaceAll(uuid.New().String(), "-", "")
	}
	prompt = CreatePrompt(prompt, id)
	generatedCode, err := s.GPTClient.SendMessage(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("failed to get code from GPT: %w", err)
	}
	log.Printf("Generated code for runtime ID: %s", id)
	extractedCode := util.ExtractGoCode(generatedCode)

	if err := util.DownloadNonStandardPackages(extractedCode, util.GetYaegiGoPath()); err != nil {
		return "", fmt.Errorf("failed to download non-standard packages: %w", err)
	}

	interp, output := util.NewYaegiInterpreter()

	runtime := &models.Runtime{
		ID:           id,
		Prompt:       prompt,
		State:        models.RSINIT,
		LastErrorMsg: "",
		RebuildCount: 0,
		Code:         extractedCode,
		CreatedAt:    time.Now(),
		Executer:     interp,
		Logs:         output,
	}
	s.Runtimes.Store(runtime.ID, runtime)
	if err := s.SaveExecuter(ctx, runtime); err != nil {
		return "", fmt.Errorf("failed to save runtime: %w", err)
	}

	if err := code.DefaultValidator(id).Validate(extractedCode); err != nil {
		log.Printf("Code validation failed for runtime ID: %s, error: %v", runtime.ID, err)
		runtime.LastErrorMsg = fmt.Sprintf("code validation failed: %v", err)
		runtime.State = "error"
		s.Runtimes.Store(runtime.ID, runtime)
		go s.HandleRuntimeFailure(ctx, id)
		return "", fmt.Errorf("code validation failed: %v", err)
	}
	return runtime.ID, nil
}

func (s *ExecuterService) ExecuteRuntime(ctx context.Context, runtimeID string) error {
	log.Printf("Executing runtime: %s", runtimeID)
	runtime, ok := s.Runtimes.Load(runtimeID)
	if !ok {
		return fmt.Errorf("runtime not found: %s", runtimeID)
	}
	runtimeData := runtime.(*models.Runtime)
	runtimeData.State = models.RSRUN
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
				s.HandleRuntimeFailure(ctx, runtimeID)
			} else {
				log.Printf("Runtime finished successfully for executer with ID: %s", runtimeID)
				runtimeData.State = "finished"
				runtimeData.FinishedAt = time.Now()
				s.Runtimes.Store(runtimeID, runtimeData)
			}
			cancel()
		}()
		execDone := make(chan error, 1)
		isRegistered := false
		go func() {
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
						time.Sleep(10 * time.Second)
						if !util.RuntimeHealthCheck(runtimeID) {
							log.Printf("Runtime health check failed for executer with ID: %s", runtimeID)
							runtimeData.LastErrorMsg = "runtime root endpoint was inaccessible"
							runtimeData.State = "error"
							s.Runtimes.Store(runtimeID, runtimeData)
							go s.HandleRuntimeFailure(ctx, runtimeID)
							cancel()
						} else {
							log.Printf("Runtime health check passed for executer with ID: %s", runtimeID)
							runtimeData.PassedHealthCheck = true
							s.Runtimes.Store(runtimeID, runtimeData)
						}
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
						time.Sleep(10 * time.Millisecond)

					}
				}
			}
		}()
		//die god panic!
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic during EvalWithContext: %v", r)
					log.Printf("Panic in EvalWithContext for executer ID: %s: %s", runtimeID, err)
				}
			}()
			log.Println("Executing code in runtime")
			go func() {
				time.Sleep(45 * time.Second)
				if !isRegistered {
					cancel()
					s.StopRuntime(ctx, runtimeID)
					log.Printf("Runtime execution timed out for executer ID: %s", runtimeID)
					err = fmt.Errorf("runtime never logged a port")
					runtimeData.LastErrorMsg = err.Error()
					runtimeData.State = "error"
					s.Runtimes.Store(runtimeID, runtimeData)
					s.HandleRuntimeFailure(ctx, runtimeID)
				}
			}()
			_, err = runtimeData.Executer.EvalWithContext(ctx2, runtimeData.Code)
		}()

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

func (s *ExecuterService) HandleRuntimeFailure(ctx context.Context, runtimeID string) error {
	// Prevent multiple retries from running concurrently.
	if _, loaded := s.ActiveRetries.LoadOrStore(runtimeID, true); loaded {
		log.Printf("Retry for runtime %s is already in progress, skipping duplicate attempt.", runtimeID)
		return nil
	}
	defer s.ActiveRetries.Delete(runtimeID) // Remove lock after retry attempt.

	// Check if the parent context is already canceled.
	select {
	case <-ctx.Done():
		return fmt.Errorf("parent context canceled, aborting failure handling for runtime %s", runtimeID)
	default:
	}

	log.Printf("Handling failure for runtime: %s", runtimeID)
	runtime, ok := s.Runtimes.Load(runtimeID)
	if !ok {
		return fmt.Errorf("runtime not found: %s", runtimeID)
	}
	runtimeData := runtime.(*models.Runtime)

	// Stop if retry limit is reached.
	if runtimeData.RebuildCount >= s.RetryLimit {
		log.Printf("Retry limit reached for runtime %s: %d attempts", runtimeID, s.RetryLimit)
		runtimeData.State = "failed"
		s.Runtimes.Store(runtimeID, runtimeData)
		s.DynamicRouteService.DeregisterReverseProxy(runtimeID)
		if _, err := s.PrepareRuntime(ctx, runtimeData.Prompt, runtimeID); err != nil {
			return fmt.Errorf("failed to prepare runtime after reaching retry limit: %w", err)
		}
		log.Printf("Rebuilding runtime %s after reaching retry limit", runtimeID)
		return s.ExecuteRuntime(ctx, runtimeID)
	}

	// Increment retry count.
	runtimeData.RebuildCount++
	log.Printf("Retrying runtime %s (attempt %d of %d)", runtimeID, runtimeData.RebuildCount, s.RetryLimit)

	// Shutdown previous runtime before retrying.
	if runtimeData.Executer != nil {
		_, _ = runtimeData.Executer.Eval("Shutdown()")
	}
	s.DynamicRouteService.DeregisterReverseProxy(runtimeID)

	// Request corrected code from GPT using the provided context.
	prompt := CreateRebuildPrompt(runtimeData.Prompt, runtimeData.LastErrorMsg, runtimeData.Code)
	code, err := s.GPTClient.SendMessage(ctx, prompt)
	if err != nil {
		return fmt.Errorf("failed to get code from GPT: %w", err)
	}

	// Rebuild runtime with corrected code.
	interp, output := util.NewYaegiInterpreter()
	extractedCode := util.ExtractGoCode(code)
	runtimeData.Code = extractedCode
	runtimeData.State = "rebuilding"
	runtimeData.LastErrorMsg = ""
	runtimeData.Executer = interp
	runtimeData.Logs = output
	s.Runtimes.Store(runtimeID, runtimeData)

	// Execute the rebuilt runtime using the parent's context.
	return s.ExecuteRuntime(ctx, runtimeID)
}
func (s *ExecuterService) GetRuntime(ctx context.Context, runtimeID string) (*models.Runtime, error) {
	runtime, ok := s.Runtimes.Load(runtimeID)
	if !ok {
		return nil, fmt.Errorf("runtime not found: %s", runtimeID)
	}
	runtimeData := runtime.(*models.Runtime)
	return runtimeData, nil
}

func (s *ExecuterService) UpdateRuntimeState(ctx context.Context, runtimeID string, state models.RuntimeState) error {
	runtime, ok := s.Runtimes.Load(runtimeID)
	if !ok {
		return fmt.Errorf("runtime not found: %s", runtimeID)
	}
	runtimeData := runtime.(*models.Runtime)
	runtimeData.State = state
	s.Runtimes.Store(runtimeID, runtimeData)
	return nil
}

func (s *ExecuterService) SaveExecuter(ctx context.Context, runtime *models.Runtime) error {
	log.Printf("Saving runtime data for ID: %s", runtime.ID)
	data, err := json.Marshal(runtime)
	if err != nil {
		return fmt.Errorf("failed to marshal runtime data: %w", err)
	}
	if err := os.MkdirAll(s.Config.ExecuterStore, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	f, err := os.Create(s.Config.ExecuterStore + "/" + runtime.ID + ".json")
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	os.Remove(s.Config.ExecuterStore + "/._" + runtime.ID + ".json")
	defer f.Close()
	_, err = f.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write data to file: %w", err)
	}
	return nil
}

func (s *ExecuterService) LoadExecuter(ctx context.Context, runtimeID string) (*models.Runtime, error) {
	f, err := os.Open(s.Config.ExecuterStore + "/" + runtimeID + ".json")
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()
	var runtime models.Runtime
	err = json.NewDecoder(f).Decode(&runtime)
	if err != nil {
		return nil, fmt.Errorf("failed to decode data from file: %w", err)
	}
	return &runtime, nil
}

func (s *ExecuterService) LoadAllExecuters(ctx context.Context) ([]*models.Runtime, error) {
	files, err := os.ReadDir(s.Config.ExecuterStore)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}
	var runtimes []*models.Runtime
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".json") {
			runtime, err := s.LoadExecuter(ctx, strings.TrimRight(file.Name(), ".json"))
			if err != nil {
				log.Printf("failed to load runtime %s: %v", file.Name(), err)
				continue
			}
			s.Runtimes.Store(runtime.ID, runtime)
		}
	}
	return runtimes, nil
}
