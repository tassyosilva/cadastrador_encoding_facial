package main

import (
	"encoding/base64"
	"encoding/gob"
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

// FaceEncoding representa o encoding de um rosto
type FaceEncoding struct {
	Data  []float32
	Shape []int
}

// EncodingResult é a resposta do script Python
type EncodingResult struct {
	Success  bool   `json:"success"`
	Encoding string `json:"encoding,omitempty"`
	Shape    []int  `json:"shape,omitempty"`
	Error    string `json:"error,omitempty"`
}

// KnownFaces armazena os encodings e nomes
type KnownFaces struct {
	Encodings [][]float32
	Names     []string
}

func main() {
	knownDir := "/fotosconhecidas"
	outputFile := "../known_faces.gob"
	
	// Verificar argumentos da linha de comando
	if len(os.Args) > 1 {
		knownDir = os.Args[1]
	}
	if len(os.Args) > 2 {
		outputFile = os.Args[2]
	}

	fmt.Printf("Processando imagens de %s e salvando em %s\n", knownDir, outputFile)
	
	// Processar as faces conhecidas
	err := preprocessKnownFaces(knownDir, outputFile)
	if err != nil {
		fmt.Printf("Erro: %v\n", err)
		os.Exit(1)
	}
}

func preprocessKnownFaces(knownDir, outputFile string) error {
	// Verificar se o diretório existe
	if _, err := os.Stat(knownDir); os.IsNotExist(err) {
		return fmt.Errorf("o diretório %s não existe", knownDir)
	}

	// Carregar dados existentes, se houver
	var knownFaces KnownFaces
	knownNamesMap := make(map[string]bool)

	if _, err := os.Stat(outputFile); err == nil {
		// O arquivo existe, carregar dados
		file, err := os.Open(outputFile)
		if err != nil {
			return fmt.Errorf("erro ao abrir arquivo %s: %v", outputFile, err)
		}
		defer file.Close()

		decoder := gob.NewDecoder(file)
		if err := decoder.Decode(&knownFaces); err != nil {
			return fmt.Errorf("erro ao decodificar arquivo %s: %v", outputFile, err)
		}
		
		fmt.Printf("Carregados %d encodings existentes\n", len(knownFaces.Names))
	}

	// Adicionar nomes existentes ao mapa para verificação rápida
	for _, name := range knownFaces.Names {
		knownNamesMap[name] = true
	}

	// Listar arquivos no diretório
	files, err := os.ReadDir(knownDir)
	if err != nil {
		return fmt.Errorf("erro ao ler diretório %s: %v", knownDir, err)
	}

	fmt.Printf("Encontradas %d imagens para processar\n", len(files))

	// Configurar processamento paralelo
	numWorkers := runtime.NumCPU()
	fmt.Printf("Utilizando %d workers (CPUs)\n", numWorkers)
	
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

	for i := 0; i < validFiles; i++ {
		result := <-results
		processed++
		
		if result.err != nil {
			fmt.Printf("Erro ao processar %s: %v\n", result.name, result.err)
			continue
		}
		
		knownFaces.Encodings = append(knownFaces.Encodings, result.encoding)
		knownFaces.Names = append(knownFaces.Names, result.name)
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
		fmt.Printf("Salvando %d encodings (%d novos) em %s...\n", len(knownFaces.Names), newEncodings, outputFile)
		
		// Criar diretório de saída se não existir
		outputDir := filepath.Dir(outputFile)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("erro ao criar diretório %s: %v", outputDir, err)
		}
		
		file, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("erro ao criar arquivo %s: %v", outputFile, err)
		}
		defer file.Close()
		
		encoder := gob.NewEncoder(file)
		if err := encoder.Encode(knownFaces); err != nil {
			return fmt.Errorf("erro ao codificar dados: %v", err)
		}
		
		fmt.Println("Encodings salvos com sucesso!")
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
	}
}

func extractFaceEncoding(imagePath string) ([]float32, error) {
	// Chamar o script Python para extrair o encoding
	cmd := exec.Command("python3", "scripts/face_encoder.py", imagePath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("erro ao criar pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("erro ao iniciar script Python: %v", err)
	}

	// Ler a saída do script
	output, err := io.ReadAll(stdout)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler saída: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("erro ao executar script Python: %v", err)
	}

	// Processar o resultado JSON
	var result EncodingResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("erro ao processar JSON: %v", err)
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
