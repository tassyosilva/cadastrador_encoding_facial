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