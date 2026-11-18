// Header-authenticated fetch streaming; no URL contains session proof.
export async function connectOverview(request, receive, status, signal) {
  let cursor = '';
  let backoff = 500;
  while (!signal.aborted) {
    let reader;
    const attempt = new AbortController();
    const abort = () => attempt.abort();
    signal.addEventListener('abort', abort, {once: true});
    globalThis.addEventListener?.('offline', abort, {once: true});
    const timeout = setTimeout(abort, 75000);
    try {
      status('Connecting…');
      const headers = cursor ? {'Last-Event-ID': cursor} : {};
      const response = await request('/api/updates', {headers, signal: attempt.signal});
      if (response.status === 401 || response.status === 403) {
        status('Session expired or denied. Reopen a current local session.');
        return;
      }
      if (!response.ok || !response.headers.get('Content-Type')
        ?.startsWith('text/event-stream')) throw new Error('Unavailable');
      status('Connected');
      reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      for (;;) {
        const {value, done} = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, {stream: true});
        if (buffer.length > 4 * 1024 * 1024) throw new Error('Update limit');
        let boundary;
        while ((boundary = buffer.indexOf('\n\n')) >= 0) {
          const frame = buffer.slice(0, boundary);
          buffer = buffer.slice(boundary + 2);
          const data = frame.split('\n').find(line => line.startsWith('data: '));
          const id = frame.split('\n').find(line => line.startsWith('id: '));
          if (!data || !id) continue;
          const update = JSON.parse(data.slice(6));
          if (update.kind !== 'reset' || update.overview?.schema_version !== 1 ||
            String(update.id) !== id.slice(4)) throw new Error('Invalid update');
          receive(update.overview);
          cursor = id.slice(4);
          backoff = 500;
          status('Connected');
        }
      }
    } catch {
      if (signal.aborted) return;
    } finally {
      clearTimeout(timeout);
      signal.removeEventListener('abort', abort);
      globalThis.removeEventListener?.('offline', abort);
      await reader?.cancel().catch(() => {});
      reader?.releaseLock();
    }
    if (signal.aborted) return;
    status('Disconnected; retaining observations. Reconnecting…');
    await new Promise(resolve => {
      const finish = () => {
        clearTimeout(timer);
        signal.removeEventListener('abort', finish);
        resolve();
      };
      const timer = setTimeout(finish, backoff);
      signal.addEventListener('abort', finish, {once: true});
    });
    backoff = Math.min(backoff * 2, 10000);
  }
}
