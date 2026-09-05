const STORAGE_KEY = 'tts-studio.playback.v1';

export class PlaybackMemory {
  constructor(storage) {
    this.storage = storage;
    this.data = { selectedJobId: null, books: {} };
    try {
      const saved = JSON.parse(storage.getItem(STORAGE_KEY));
      if (saved && typeof saved.books === 'object' && saved.books !== null) {
        this.data.selectedJobId = typeof saved.selectedJobId === 'string' ? saved.selectedJobId : null;
        for (const [id, mark] of Object.entries(saved.books)) {
          if (Number.isInteger(mark?.chunkId) && Number.isFinite(mark.position) && mark.position >= 0 && typeof mark.audioURL === 'string') {
            this.data.books[id] = mark;
          }
        }
      }
    } catch { /* Playback remains usable when browser storage is unavailable. */ }
  }
  flush() { try { this.storage.setItem(STORAGE_KEY, JSON.stringify(this.data)); } catch {} }
  select(jobId) { this.data.selectedJobId = jobId; this.flush(); }
  get(jobId) { return this.data.books[jobId]; }
  remember(jobId, chunkId, audioURL, position) {
    if (!Number.isFinite(position) || position < 0) return;
    this.data.books[jobId] = { chunkId, audioURL, position };
    this.flush();
  }
  update(jobId, chunkId, audioURL, position) {
    const mark = this.get(jobId);
    // Late pause/timeupdate events from the previous fragment cannot move the bookmark back.
    if (mark?.chunkId === chunkId && mark.audioURL === audioURL) this.remember(jobId, chunkId, audioURL, position);
  }
}
