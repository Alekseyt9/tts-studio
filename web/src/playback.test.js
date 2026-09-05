import { test } from 'node:test';
import assert from 'node:assert/strict';
import { PlaybackMemory } from './playback.js';

function storage() {
  let value = null;
  return { getItem: () => value, setItem: (_, next) => { value = next; } };
}
test('each book and selected job survive a reload', () => {
  const disk = storage(), first = new PlaybackMemory(disk);
  first.select('book-one');
  first.remember('book-one', 12, '/one/12.wav', 34.75);
  first.remember('book-two', 3, '/two/3.wav', 10);
  const restored = new PlaybackMemory(disk);
  assert.equal(restored.data.selectedJobId, 'book-one');
  assert.deepEqual(restored.get('book-one'), {chunkId:12,audioURL:'/one/12.wav',position:34.75});
  assert.equal(restored.get('book-two').position, 10);
});
test('late events from a previous fragment or replaced file cannot reset current position', () => {
  const memory = new PlaybackMemory(storage());
  memory.remember('book', 2, '/2.wav', 0);
  memory.update('book', 1, '/1.wav', 60);
  memory.update('book', 2, '/old-2.wav', 99);
  assert.equal(memory.get('book').position, 0);
  memory.update('book', 2, '/2.wav', 4.5);
  assert.equal(memory.get('book').position, 4.5);
});
test('broken or blocked storage does not prevent playback', () => {
  const broken = {getItem:()=>'{broken',setItem:()=>{throw new Error('quota')}};
  const memory = new PlaybackMemory(broken);
  memory.remember('book', 1, '/1.wav', 2);
  memory.update('book', 1, '/1.wav', NaN);
  assert.equal(memory.get('book').position, 2);
  assert.doesNotThrow(()=>new PlaybackMemory(null).remember('book',1,'/1.wav',5));
});
