import face_recognition
import sys
import json
import base64
import numpy as np

def encode_face(image_path):
    try:
        # Imprimir informações para debug
        print(f"Tentando carregar: {image_path}", file=sys.stderr)
        
        # Tentar carregar a imagem
        try:
            image = face_recognition.load_image_file(image_path)
        except Exception as e:
            # Tentar com PIL como alternativa
            try:
                from PIL import Image
                import numpy as np
                pil_image = Image.open(image_path)
                image = np.array(pil_image)
            except Exception as e2:
                return json.dumps({
                    "success": False, 
                    "error": f"Falha ao carregar imagem: {str(e)}; Tentativa PIL: {str(e2)}"
                })
        
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