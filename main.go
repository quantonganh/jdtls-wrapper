package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type jdtResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result"`
}

type stdRequest struct {
	Jsonrpc string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  Params          `json:"params"`
	ID      json.RawMessage `json:"id"`
}

type Params struct {
	Position     Position     `json:"position"`
	TextDocument TextDocument `json:"textDocument"`
}

type TextDocument struct {
	URI string `json:"uri"`
}

type stdResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Result  []stdResult `json:"result"`
}

type jdtResult struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type stdResult struct {
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Position struct {
	Line      int64 `json:"line"`
	Character int64 `json:"character"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cmd := exec.Command("jdtls", os.Args[1:]...)

	serverStdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	serverStdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	serverStderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Forward client -> server
	m := make(map[string]string)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			body, err := readLSPMessage(reader)
			if err != nil {
				fmt.Fprintln(os.Stderr, "[jdtls-wrapper]", err.Error())
			}

			var stdReq *stdRequest
			if err := json.Unmarshal(body, &stdReq); err != nil {
				fmt.Fprintln(os.Stderr, "[jdtls-wrapper]", err.Error())
			}

			if jdtURI, ok := m[stdReq.Params.TextDocument.URI]; ok {
				stdReq.Params.TextDocument.URI = jdtURI
				body, _ = json.Marshal(stdReq)
			}

			fmt.Fprintf(serverStdin, "Content-Length: %d\r\n\r\n", len(body))
			serverStdin.Write(body)
		}
	}()

	// Forward server -> client
	pending := make(map[int]func(*jdtResponse))
	go func() {
		reader := bufio.NewReader(serverStdout)
		for {
			body, err := readLSPMessage(reader)
			if err != nil {
				fmt.Fprintln(os.Stderr, "[jdtls-wrapper]", err.Error())
			}

			fmt.Fprintln(os.Stderr, "[jdtls-wrapper]", string(body))

			var jdtResp *jdtResponse
			if err := json.Unmarshal(body, &jdtResp); err != nil {
				fmt.Fprintln(os.Stderr, "[jdtls-wrapper]", err.Error())
			}

			id, ok := getID(jdtResp.ID)
			if !ok {
				fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
				continue
			}

			if cb, ok := pending[id]; ok {
				cb(jdtResp)
				delete(pending, id)
				continue
			}

			if len(jdtResp.Result) == 0 {
				fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
				continue
			}

			var definitionResult []jdtResult
			if err := json.Unmarshal(jdtResp.Result, &definitionResult); err != nil {
				fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
				continue
			}

			if len(definitionResult) == 0 {
				fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
				continue
			}

			uri, err := url.Parse(definitionResult[0].URI)
			if err != nil {
				fmt.Fprintln(os.Stderr, "[jdtls-wrapper]: invalid uri", definitionResult[0].URI)
			}
			fmt.Fprintf(os.Stderr, "path: %s\n", uri.Path)

			if uri.Scheme == "jdt" {
				newID := id + 1
				classReq := map[string]any{
					"id":      newID,
					"jsonrpc": "2.0",
					"method":  "java/classFileContents",
					"params": map[string]any{
						"uri": definitionResult[0].URI,
					},
				}
				data, err := json.Marshal(classReq)
				if err != nil {
					fmt.Fprintln(os.Stderr, "[jdtls-wrapper]", err.Error())
				}

				pending[newID] = func(resp *jdtResponse) {
					fmt.Fprintln(os.Stderr, "[jdtls-wrapper]", string(resp.Result))
					result, err := strconv.Unquote(string(resp.Result))
					if err != nil {
						fmt.Fprintln(os.Stderr, "[jdtls-wrapper]", err.Error())
					}

					result = strings.ReplaceAll(result, `\n`, "\n")
					result = strings.ReplaceAll(result, `\t`, "\t")
					tmpFileName := os.TempDir() + strings.TrimSuffix(uri.Path, ".class") + ".java"
					var targetURI string
					if runtime.GOOS == "windows" {
						targetURI = "file:///" + tmpFileName
					} else {
						targetURI = "file://" + tmpFileName
					}
					m[targetURI] = uri.String()
					if err := os.MkdirAll(filepath.Dir(tmpFileName), 0755); err != nil {
						fmt.Fprintln(os.Stderr, "[jdtls-wrapper]", err.Error())
					}
					if err := os.WriteFile(tmpFileName, []byte(result), 0400); err != nil {
						fmt.Fprintln(os.Stderr, "[jdtls-wrapper]", err.Error())
					}
					targetRange := Range{
						Start: Position{
							Line:      definitionResult[0].Range.Start.Line,
							Character: definitionResult[0].Range.Start.Character,
						},
						End: Position{
							Line:      definitionResult[0].Range.End.Line,
							Character: definitionResult[0].Range.End.Character,
						},
					}
					stdResp := &stdResponse{
						Jsonrpc: "2.0",
						ID:      int64(id),
						Result: []stdResult{
							{
								TargetURI:            targetURI,
								TargetRange:          targetRange,
								TargetSelectionRange: targetRange,
							},
						},
					}

					data, err := json.Marshal(stdResp)
					if err == nil {
						fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(data), data)
					}
				}

				fmt.Fprintf(serverStdin, "Content-Length: %d\r\n\r\n%s", len(data), data)
			} else {
				fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
			}
		}
	}()

	scanner := bufio.NewScanner(serverStderr)
	go func() {
		for scanner.Scan() {
			fmt.Fprintln(os.Stderr, "[jdtls-wrapper]", scanner.Text())
		}
	}()

	cmd.Wait()

	return nil
}

func readLSPMessage(r *bufio.Reader) (body []byte, err error) {
	var contentLength int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "\r\n" {
			break
		}

		if strings.HasPrefix(line, "Content-Length:") {
			lengthStr := strings.TrimSpace(line[len("Content-Length:"):])
			length, err := strconv.Atoi(lengthStr)
			if err != nil {
				return nil, err
			}
			contentLength = length
		}
	}

	buf := make([]byte, contentLength)
	_, err = io.ReadFull(r, buf)
	if err != nil {
		return nil, err
	}

	return buf, nil
}

func getID(id interface{}) (int, bool) {
	switch v := id.(type) {
	case float64:
		return int(v), true
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n, true
		}
		return 0, false
	default:
		return 0, false
	}
}
