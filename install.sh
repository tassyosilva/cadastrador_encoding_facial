#!/bin/bash
set -e
echo "Instalando Go 1.22..."
wget -q https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
rm go1.22.0.linux-amd64.tar.gz
# Adicionar Go ao PATH para a sessão atual e permanentemente
export PATH=$PATH:/usr/local/go/bin
# Adicionar ao .profile para sessões futuras
if ! grep -q "/usr/local/go/bin" ~/.profile; then
    echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
fi
# Verificar a instalação do Go
if command -v go &> /dev/null; then
    echo "Go $(go version) instalado com sucesso!"
else
    echo "Go instalado em /usr/local/go/bin, mas não está no PATH."
    echo "Usando caminho completo para comandos Go."
    GO_BIN="/usr/local/go/bin/go"
fi
# Definir o comando Go (usando variável que funciona com ou sem Go no PATH)
GO_CMD="go"
if ! command -v go &> /dev/null; then
    GO_CMD="/usr/local/go/bin/go"
fi
# Verificar e instalar dependências Python
echo "Verificando dependências Python..."
if ! python3 -c "import face_recognition" &> /dev/null; then
    echo "Instalando face_recognition..."
    pip3 install face_recognition
fi
if ! python3 -c "import numpy" &> /dev/null; then
    echo "Instalando numpy..."
    pip3 install numpy
fi
# Verificar se o script Python existe
if [ ! -f "scripts/face_encoder.py" ]; then
    echo "Criando script face_encoder.py..."
    mkdir -p scripts
    cat > scripts/face_encoder.py << 'EOF'
import face_recognition
import sys
import json
import base64
import numpy as np

def encode_face(image_path):
    try:
        # Carregar a imagem
        image = face_recognition.load_image_file(image_path)
        
        # Extrair encodings
        encodings = face_recognition.face_encodings(image)
        
        if not encodings:
            return json.dumps({"success": False, "error": "No face found"})
            
        # Converter o primeiro encoding para uma lista e depois para JSON
        # Precisamos converter para base64 porque numpy arrays não são serializáveis diretamente
        encoding_base64 = base64.b64encode(encodings[0].tobytes()).decode('utf-8')
        
        return json.dumps({
            "success": True,
            "encoding": encoding_base64,
            "shape": encodings[0].shape
        })
    except Exception as e:
        return json.dumps({"success": False, "error": str(e)})

if __name__ == "__main__":
    if len(sys.argv) != 2:
        print(json.dumps({"success": False, "error": "Usage: python face_encoder.py <image_path>"}))
        sys.exit(1)
        
    image_path = sys.argv[1]
    result = encode_face(image_path)
    print(result)
EOF
fi
echo "Compilando o processador de faces..."
mkdir -p bin
$GO_CMD build -o bin/face-processor ./cmd/processor
echo "Instalação concluída!"
echo "Para executar: ./bin/face-processor /fotosconhecidas /fotoscodificadas"
echo ""
echo "Isso irá processar as imagens em /fotosconhecidas e salvar os encodings em /fotoscodificadas"
echo "no formato compatível com o sistema de reconhecimento facial existente."