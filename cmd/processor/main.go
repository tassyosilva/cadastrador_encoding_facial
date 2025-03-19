package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// EncodingResult é a resposta do script Python
type EncodingResult struct {
	Success  bool   `json:"success"`
	Encoding string `json:"encoding,omitempty"`
	Shape    []int  `json:"shape,omitempty"`
	Error    string `json:"error,omitempty"`
}

func main() {
	knownDir := "/fotosconhecidas"
	outputDir := "/fotoscodificadas"
	
	// Verificar argumentos da linha de comando
	if len(os.Args) > 1 {
		knownDir = os.Args[1]
	}
	if len(os.Args) > 2 {
		outputDir = os.Args[2]
	}

	fmt.Printf("Processando imagens de %s e salvando em %s\n", knownDir, outputDir)
	
	// Processar as faces conhecidas
	err := preprocessKnownFaces(knownDir, outputDir)
	if err != nil {
		fmt.Printf("Erro: %v\n", err)
		os.Exit(1)
	}
}

func preprocessKnownFaces(knownDir, outputDir string) error {
	// Verificar se o diretório existe
	if _, err := os.Stat(knownDir); os.IsNotExist(err) {
		return fmt.Errorf("o diretório %s não existe", knownDir)
	}

	// Criar diretório de saída se não existir
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("erro ao criar diretório %s: %v", outputDir, err)
	}

	// Carregar dados existentes, se houver
	var knownEncodings [][]float32
	var knownNames []string
	knownNamesMap := make(map[string]bool)

	encodingsFile := filepath.Join(outputDir, "encodings.npy")
	namesFile := filepath.Join(outputDir, "names.npy")

	if _, err1 := os.Stat(encodingsFile); err1 == nil {
		if _, err2 := os.Stat(namesFile); err2 == nil {
			// Os arquivos existem, carregar dados usando Python
			fmt.Println("Carregando encodings existentes...")
			
			cmd := exec.Command("python3", "-c", `
import numpy as np
import sys
import json

try:
    encodings = np.load("` + encodingsFile + `", allow_pickle=True)
    names = np.load("` + namesFile + `", allow_pickle=True)
    print(json.dumps({"success": True, "count": len(names), "names": names.tolist()}))
except Exception as e:
    print(json.dumps({"success": False, "error": str(e)}))
`)
			
			output, err := cmd.Output()
			if err != nil {
				fmt.Printf("Aviso: erro ao carregar encodings existentes: %v\n", err)
			} else {
				var result struct {
					Success bool     `json:"success"`
					Count   int      `json:"count"`
					Names   []string `json:"names"`
					Error   string   `json:"error"`
				}
				
				if err := json.Unmarshal(output, &result); err != nil {
					fmt.Printf("Aviso: erro ao processar resposta: %v\n", err)
				} else if result.Success {
					knownNames = result.Names
					fmt.Printf("Carregados %d encodings existentes\n", result.Count)
					
					// Preencher o mapa de nomes
					for _, name := range knownNames {
						knownNamesMap[name] = true
					}
				} else {
					fmt.Printf("Aviso: erro ao carregar encodings: %s\n", result.Error)
				}
			}
		}
	}

	// Listar arquivos no diretório
	files, err := os.ReadDir(knownDir)
	if err != nil {
		return fmt.Errorf("erro ao ler diretório %s: %v", knownDir, err)
	}

	// Verificar nomes de arquivos problemáticos
	fmt.Println("Verificando nomes de arquivos...")
	problematicFiles := 0
	for _, file := range files {
		fileName := file.Name()
		if strings.Contains(fileName, " ") || strings.Contains(fileName, "'") || 
		   strings.Contains(fileName, "&") || strings.Contains(fileName, "#") {
			fmt.Printf("Aviso: O arquivo '%s' contém espaços ou caracteres especiais\n", fileName)
			problematicFiles++
		}
	}
	if problematicFiles > 0 {
		fmt.Printf("Encontrados %d arquivos com nomes problemáticos que podem causar erros\n", problematicFiles)
	}

	// Contar arquivos por extensão
	jpgCount := 0
	jpegCount := 0
	pngCount := 0
	otherCount := 0

	for _, file := range files {
		fileName := file.Name()
		ext := strings.ToLower(filepath.Ext(fileName))
		
		switch ext {
		case ".jpg":
			jpgCount++
		case ".jpeg":
			jpegCount++
		case ".png":
			pngCount++
		default:
			otherCount++
			fmt.Printf("Extensão não suportada: %s (%s)\n", ext, fileName)
		}
	}

	fmt.Printf("Contagem de arquivos por extensão: .jpg: %d, .jpeg: %d, .png: %d, outros: %d\n",
			jpgCount, jpegCount, pngCount, otherCount)

	fmt.Printf("Encontradas %d imagens para processar\n", len(files))

	// Configurar processamento paralelo
	cpuCount := runtime.NumCPU()
	numWorkers := cpuCount * 2  // Multiplicador ajustável para melhor desempenho
	fmt.Printf("Utilizando %d workers (com %d CPUs disponíveis)\n", numWorkers, cpuCount)
	
	jobs := make(chan string, len(files))
	results := make(chan struct {
		name     string
		encoding []float32
		err      error
	}, len(files))

	// Iniciar workers
	var wg sync.WaitGroup
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go worker(w, knownDir, jobs, results, &wg)
	}

	// Enviar trabalhos
	validFiles := 0
	for _, file := range files {
		fileName := file.Name()
		ext := strings.ToLower(filepath.Ext(fileName))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" {
			if _, exists := knownNamesMap[fileName]; !exists {
				jobs <- fileName
				validFiles++
			} else {
				fmt.Printf("Imagem %s já processada, ignorando...\n", fileName)
			}
		}
	}
	close(jobs)

	// Coletar resultados
	fmt.Printf("Processando %d imagens válidas...\n", validFiles)
	startTime := time.Now()
	processed := 0
	newEncodings := 0

	// Coletar novos encodings
	var newEncodingsList [][]float32
	var newNamesList []string

	for i := 0; i < validFiles; i++ {
		result := <-results
		processed++
		
		if result.err != nil {
			fmt.Printf("Erro ao processar %s: %v\n", result.name, result.err)
			continue
		}
		
		newEncodingsList = append(newEncodingsList, result.encoding)
		newNamesList = append(newNamesList, result.name)
		newEncodings++
		
		// Mostrar progresso
		elapsed := time.Since(startTime)
		imagesPerSecond := float64(processed) / elapsed.Seconds()
		fmt.Printf("\rProcessado: %d/%d (%.2f imagens/seg)", processed, validFiles, imagesPerSecond)
	}
	fmt.Println()

	// Aguardar todos os workers terminarem
	wg.Wait()
	close(results)

	// Salvar resultados se houver novos encodings
	if newEncodings > 0 {
		fmt.Printf("Salvando %d encodings novos...\n", newEncodings)
		
		// Combinar encodings existentes com novos
		allEncodings := append(knownEncodings, newEncodingsList...)
		allNames := append(knownNames, newNamesList...)
		
		// Salvar usando Python para manter compatibilidade
		tempEncodingsFile := filepath.Join(outputDir, "temp_encodings.json")
		tempNamesFile := filepath.Join(outputDir, "temp_names.json")
		
		// Salvar encodings em JSON temporário
		encodingsJSON, err := json.Marshal(allEncodings)
		if err != nil {
			return fmt.Errorf("erro ao serializar encodings: %v", err)
		}
		
		if err := os.WriteFile(tempEncodingsFile, encodingsJSON, 0644); err != nil {
			return fmt.Errorf("erro ao salvar encodings temporários: %v", err)
		}
		
		// Salvar nomes em JSON temporário
		namesJSON, err := json.Marshal(allNames)
		if err != nil {
			return fmt.Errorf("erro ao serializar nomes: %v", err)
		}
		
		if err := os.WriteFile(tempNamesFile, namesJSON, 0644); err != nil {
			return fmt.Errorf("erro ao salvar nomes temporários: %v", err)
		}
		
		// Converter JSON para NPY usando Python
		cmd := exec.Command("python3", "-c", `
import numpy as np
import json

# Carregar encodings do JSON
with open("` + tempEncodingsFile + `", "r") as f:
    encodings = json.load(f)

# Carregar nomes do JSON
with open("` + tempNamesFile + `", "r") as f:
    names = json.load(f)

# Salvar como NPY
np.save("` + encodingsFile + `", np.array(encodings))
np.save("` + namesFile + `", np.array(names))

# Remover arquivos temporários
import os
os.remove("` + tempEncodingsFile + `")
os.remove("` + tempNamesFile + `")
`)
		
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("erro ao converter para NPY: %v", err)
		}
		
		fmt.Printf("Encodings salvos com sucesso em %s e %s\n", encodingsFile, namesFile)
	} else {
		fmt.Println("Nenhum novo encoding para salvar.")
	}

	// Mostrar estatísticas finais
	if processed > 0 {
		totalTime := time.Since(startTime)
		avgTimePerImage := totalTime.Seconds() / float64(processed)
		fmt.Printf("Tempo total: %v\n", totalTime)
		fmt.Printf("Tempo médio por imagem: %.2f segundos\n", avgTimePerImage)
		fmt.Printf("Velocidade média: %.2f imagens/segundo\n", 1/avgTimePerImage)
	}

	fmt.Println("Processamento concluído com sucesso!")
	return nil
}

func worker(id int, knownDir string, jobs <-chan string, results chan<- struct {
	name     string
	encoding []float32
	err      error
}, wg *sync.WaitGroup) {
	defer wg.Done()
	
	// Adicionar monitoramento de desempenho do worker
	workerStartTime := time.Now()
	processedCount := 0
	
	for fileName := range jobs {
		imagePath := filepath.Join(knownDir, fileName)
		encoding, err := extractFaceEncoding(imagePath)
		
		results <- struct {
			name     string
			encoding []float32
			err      error
		}{
			name:     fileName,
			encoding: encoding,
			err:      err,
		}
		
		// Monitorar desempenho individual
		processedCount++
		if processedCount % 10 == 0 {
			elapsed := time.Since(workerStartTime)
			avgTimePerItem := elapsed.Seconds() / float64(processedCount)
			fmt.Printf("\nWorker %d: %d imagens em %.2fs (%.2f img/s)", 
					  id, processedCount, elapsed.Seconds(), 1/avgTimePerItem)
		}
	}
}

func extractFaceEncoding(imagePath string) ([]float32, error) {
	// Não é necessário escapar o caminho, a biblioteca exec já lida com espaços e caracteres especiais
	scriptPath := filepath.Join("scripts", "face_encoder.py")

	// Verificar se o script existe
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("script Python não encontrado: %s", scriptPath)
	}

	// Chamar o script Python para extrair o encoding
	cmd := exec.Command("python3", scriptPath, imagePath)
	
	// Configurar ambiente para melhor compatibilidade
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("erro ao criar pipe: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("erro ao criar pipe de erro: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("erro ao iniciar script Python: %v", err)
	}

	// Ler a saída do script
	output, err := io.ReadAll(stdout)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler saída: %v", err)
	}

	// Ler erros, se houver
	errorOutput, _ := io.ReadAll(stderr)
	
	if err := cmd.Wait(); err != nil {
		errorMsg := string(errorOutput)
		if errorMsg != "" {
			return nil, fmt.Errorf("erro ao executar script Python: %v - %s", err, errorMsg)
		}
		return nil, fmt.Errorf("erro ao executar script Python: %v", err)
	}

	// Processar o resultado JSON
	var result EncodingResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("erro ao processar JSON: %v - output: %s", err, string(output[:100]))
	}

	if !result.Success {
		return nil, fmt.Errorf("falha no encoding: %s", result.Error)
	}

	// Decodificar o encoding de base64
	encodingBytes, err := base64.StdEncoding.DecodeString(result.Encoding)
	if err != nil {
		return nil, fmt.Errorf("erro ao decodificar base64: %v", err)
	}

	// Converter bytes para []float32
	numFloats := len(encodingBytes) / 4 // float32 = 4 bytes
	encoding := make([]float32, numFloats)
	
	// Converter bytes para float32
	for i := 0; i < numFloats; i++ {
		// Cada float32 ocupa 4 bytes
		offset := i * 4
		bits := uint32(encodingBytes[offset]) |
			uint32(encodingBytes[offset+1])<<8 |
			uint32(encodingBytes[offset+2])<<16 |
			uint32(encodingBytes[offset+3])<<24
		encoding[i] = float32FromBits(bits)
	}

	return encoding, nil
}

// float32FromBits converte um uint32 para float32 seguindo o padrão IEEE 754
func float32FromBits(bits uint32) float32 {
	return *(*float32)(unsafe.Pointer(&bits))
}