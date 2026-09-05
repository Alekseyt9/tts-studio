import argparse
import hashlib
import json
import sys
from pathlib import Path

import soundfile as sf
import torch


ROOT = Path(__file__).resolve().parent
MODEL = ROOT / "models" / "Qwen3-TTS-12Hz-1.7B-Base"
ENGINE = "qwen"


def load_model():
    torch.set_num_threads(8)
    if ENGINE == "faster":
        from faster_qwen3_tts import FasterQwen3TTS
        return FasterQwen3TTS.from_pretrained(
            str(MODEL), device="cuda", dtype=torch.bfloat16,
            attn_implementation="sdpa", max_seq_len=2048, local_files_only=True,
        )
    if ENGINE.startswith("omni"):
        from omnivoice import OmniVoice
        return OmniVoice.from_pretrained(
            str(ROOT / "models" / "OmniVoice"), device_map="cuda:0",
            dtype=torch.bfloat16, attn_implementation="sdpa", local_files_only=True,
        )
    from qwen_tts import Qwen3TTSModel
    return Qwen3TTSModel.from_pretrained(
        str(MODEL),
        device_map="cuda:0",
        dtype=torch.bfloat16,
        attn_implementation="sdpa",
        local_files_only=True,
    )


def reference_transcript(ref_audio):
    key = hashlib.sha256(Path(ref_audio).read_bytes()).hexdigest()
    target = ROOT / ".cache" / "voice-transcripts" / (key + ".txt")
    if target.exists():
        return target.read_text(encoding="utf-8").strip()
    from faster_whisper import WhisperModel
    recognizer = WhisperModel(str(ROOT / "models" / "faster-whisper-small"),
                              device="cpu", compute_type="int8", cpu_threads=4,
                              local_files_only=True)
    segments, _ = recognizer.transcribe(str(ref_audio), language="ru", beam_size=5,
                                        vad_filter=False, condition_on_previous_text=False)
    transcript = " ".join(segment.text.strip() for segment in segments).strip()
    if not transcript:
        raise ValueError("Could not transcribe the voice sample; enter its transcript")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(transcript, encoding="utf-8")
    return transcript


def synthesize(model, *, text, ref_audio, ref_text, output, language="Russian", speaker_only=False):
    if not ENGINE.startswith("omni") and not speaker_only and not ref_text:
        raise ValueError("ref_text is required unless speaker_only is enabled")
    if ENGINE.startswith("omni"):
        wavs = model.generate(text=text, language=language, ref_audio=ref_audio,
                              ref_text=ref_text or reference_transcript(ref_audio),
                              num_step=32 if ENGINE == "omni32" else 16)
        sample_rate = 24000
    else:
        options = {"xvec_only" if ENGINE == "faster" else "x_vector_only_mode": speaker_only}
        wavs, sample_rate = model.generate_voice_clone(
            text=text, language=language, ref_audio=ref_audio, ref_text=ref_text, **options)
    output = Path(output).resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    sf.write(output, wavs[0], sample_rate)
    return output, sample_rate, len(wavs[0]) / sample_rate


def serve() -> None:
    # Go sends JSONL as UTF-8. Windows otherwise opens redirected stdio using
    # the active ANSI code page (cp1251), corrupting Russian text before TTS.
    sys.stdin.reconfigure(encoding="utf-8")
    sys.stdout.reconfigure(encoding="utf-8")
    model = load_model()
    print(json.dumps({"ready": True}), flush=True)
    for line in sys.stdin:
        try:
            request = json.loads(line)
            output, sample_rate, duration = synthesize(model, **request)
            print(json.dumps({
                "ok": True,
                "output": str(output),
                "sample_rate": sample_rate,
                "duration": duration,
            }), flush=True)
        except Exception as exc:
            print(json.dumps({"ok": False, "error": str(exc)}), flush=True)


def main() -> None:
    global ENGINE
    parser = argparse.ArgumentParser(description="Russian voice cloning with Qwen3-TTS 1.7B Base")
    parser.add_argument("--server", action="store_true", help="Keep the model loaded and accept JSONL requests on stdin")
    parser.add_argument("--engine", choices=["qwen", "faster", "omni32", "omni16"], default="qwen")
    parser.add_argument("--text", help="Text to synthesize")
    parser.add_argument("--ref-audio", help="Reference WAV/MP3 file")
    parser.add_argument("--ref-text", help="Exact transcript of the reference audio")
    parser.add_argument("--output", default=str(ROOT / "output.wav"))
    parser.add_argument("--language", default="Russian")
    parser.add_argument(
        "--speaker-only",
        action="store_true",
        help="Do not require reference transcript (quality may be lower)",
    )
    args = parser.parse_args()
    ENGINE = args.engine

    if not MODEL.exists():
        raise SystemExit(f"Model not found: {MODEL}")
    if args.server:
        serve()
        return
    if not args.text or not args.ref_audio:
        parser.error("--text and --ref-audio are required")
    if not ENGINE.startswith("omni") and not args.speaker_only and not args.ref_text:
        parser.error("--ref-text is required unless --speaker-only is used")

    model = load_model()
    output, _, _ = synthesize(
        model,
        text=args.text,
        ref_audio=args.ref_audio,
        ref_text=args.ref_text,
        output=args.output,
        language=args.language,
        speaker_only=args.speaker_only,
    )
    print(f"Saved: {output}")


if __name__ == "__main__":
    main()
