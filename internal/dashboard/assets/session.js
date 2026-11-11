'use strict';
(() => {
  // Capture before loading any other script or rendering provider data.
  const proof = location.hash.slice(1);
  history.replaceState(null, '', '/');
  const valid = /^[a-f0-9]{64}$/.test(proof);
  document.addEventListener('DOMContentLoaded', async () => {
    const output = document.getElementById('snapshot');
    const button = document.getElementById('connect');
    let view;
    try {
      const module = await import('/view.js');
      view = module.dashboard();
    } catch {
      output.textContent = 'Packaged dashboard unavailable.';
      return;
    }
    button.disabled = !valid;
    output.textContent = valid ? 'Ready. Select Read snapshot to connect.' :
      'Session proof unavailable. Open a current local session link.';
    button.addEventListener('click', async () => {
      button.disabled = true;
      output.textContent = 'Reading snapshot…';
      try {
        const response = await fetch('/api/snapshot', {
          headers: {'X-Nibfin-Session': proof},
          credentials: 'omit', cache: 'no-store', redirect: 'error'
        });
        if (!response.ok) throw new Error('Unavailable');
        view.load(await response.json());
        output.textContent = 'Snapshot loaded. Independent statuses apply.';
      } catch {
        view.failed();
        output.textContent = 'Snapshot unavailable or unusable. ' +
          'Any retained display is previous data. ' +
          'Reopen a current local session if needed.';
      } finally {
        button.disabled = !valid;
      }
    });
  });
})();
