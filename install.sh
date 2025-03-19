#!/bin/bash
set -e

echo "Instalando Go 1.22..."
wget -q https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
rm go1.22.0.linux-amd64.tar.gz

# Adicionar Go ao PATH se ainda não estiver
if ! grep -q "/usr/local/go/bin" ~/.profile; then
    echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
    source ~/.profile
fi

echo "Go $(go version) instalado com sucesso!"

echo "Compilando o processador de faces..."
mkdir -p bin
go build -o bin/face-processor ./cmd/processor

echo "Instalação concluída!"
echo "Para executar: ./bin/face-processor [diretório_imagens] [arquivo_saída]"
