import { useEffect, useRef } from 'react';

export function ChunkAudio({ jobId, chunk, memory, audioRefs, onPlay, onEnded, onBookmark }) {
  const element = useRef(null);
  const callbacks = useRef({ onPlay, onEnded, onBookmark });
  callbacks.current = { onPlay, onEnded, onBookmark };
  const mark = memory.get(jobId);
  const saved = useRef(mark?.chunkId === chunk.id && mark.audioURL === chunk.audio_url ? mark.position : 0);
  const restored = useRef(saved.current === 0);
  const restoringSeek = useRef(false);
  const save = audio => {
    if (restored.current) memory.update(jobId, chunk.id, chunk.audio_url, audio.currentTime);
  };

  useEffect(() => {
    const audio = element.current;
    const key = `${jobId}:${chunk.id}`;
    audioRefs.current.set(key, audio);
    const flush = () => save(audio);
    window.addEventListener('pagehide', flush);
    document.addEventListener('visibilitychange', flush);
    return () => {
      flush();
      audioRefs.current.delete(key);
      window.removeEventListener('pagehide', flush);
      document.removeEventListener('visibilitychange', flush);
    };
  }, [jobId, chunk.id, chunk.audio_url, memory, audioRefs]);

  return <audio ref={element} controls aria-label={`Слушать фрагмент ${chunk.id}`}
    preload={saved.current > 0 ? 'metadata' : 'none'} src={chunk.audio_url}
    onLoadedMetadata={event => {
      if (!restored.current) {
        const audio = event.currentTarget;
        restoringSeek.current = true;
        audio.currentTime = Math.min(saved.current, Number.isFinite(audio.duration) ? audio.duration : saved.current);
        restored.current = true;
      }
    }}
    onPlay={event => {
      const audio = event.currentTarget;
      if (restored.current) memory.remember(jobId, chunk.id, chunk.audio_url, audio.currentTime);
      callbacks.current.onPlay();
      callbacks.current.onBookmark();
    }}
    onTimeUpdate={event => save(event.currentTarget)}
    onSeeked={event => {
      if (restoringSeek.current) {
        restoringSeek.current = false;
        save(event.currentTarget);
      } else if (restored.current) memory.remember(jobId, chunk.id, chunk.audio_url, event.currentTarget.currentTime);
      callbacks.current.onBookmark();
    }}
    onPause={event => { save(event.currentTarget); callbacks.current.onBookmark(); }}
    onEnded={event => { save(event.currentTarget); callbacks.current.onEnded(); }}
  />;
}
