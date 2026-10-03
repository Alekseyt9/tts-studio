"""Apply sknyazev's Russian stress LoRA to a disposable Faster Qwen instance.

Source: https://huggingface.co/sknyazev/qwen3-tts-12hz-1.7b-ru-stress-gguf
Adapter license: Apache-2.0. Merge rule matches voicy/scripts/stress_ft_merge.py.
"""
import hashlib
import json
from pathlib import Path

ADAPTER_SHA256 = '702338d0e81ef8d04715ebd19ebe5eaf43b83446c04ee37f1a9108a9235f2b99'


def apply_russian_stress_adapter(model, adapter_dir):
    import torch
    from safetensors.torch import load_file

    adapter_dir = Path(adapter_dir)
    path = adapter_dir / 'adapter.safetensors'
    if hashlib.sha256(path.read_bytes()).hexdigest() != ADAPTER_SHA256:
        raise ValueError('Russian stress adapter checksum mismatch')
    config = json.loads((adapter_dir / 'adapter.json').read_text(encoding='utf-8'))
    if (config['base'] != 'Qwen/Qwen3-TTS-12Hz-1.7B-Base'
            or config['applied_to'] != 'talker.model'
            or config['r'] != 16 or config['alpha'] != 32):
        raise ValueError('Unexpected Russian stress adapter configuration')
    pairs = {}
    for key, tensor in load_file(str(path)).items():
        stem, part = key.rsplit('.lora_', 1)
        pairs.setdefault(stem, {})[part.split('.')[0]] = tensor
    if len(pairs) != 196:
        raise ValueError('Incomplete Russian stress adapter')
    talker = model.model.model.talker.model
    for stem, ab in pairs.items():
        weight = talker.get_submodule(stem).weight
        if (set(ab) != {'A', 'B'} or ab['A'].shape[0] != config['r']
                or ab['B'].shape[1] != config['r']
                or (ab['B'].shape[0], ab['A'].shape[1]) != tuple(weight.shape)):
            raise ValueError(f'Incompatible LoRA target: {stem}')
    with torch.no_grad():
        for stem, ab in pairs.items():
            weight = talker.get_submodule(stem).weight
            delta = ((ab['B'].float() @ ab['A'].float())
                     * (config['alpha'] / config['r'])).to(weight.device)
            weight.copy_((weight.float() + delta).to(weight.dtype))
    return len(pairs)
