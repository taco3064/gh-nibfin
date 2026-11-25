import { setNonce } from 'get-nonce'

// applyCspNonce hands the page's CSP nonce to the <style> elements Radix
// creates (react-style-singleton reads it through get-nonce). The server puts
// a fresh nonce in meta[property=csp-nonce] of every page response.
export function applyCspNonce(doc: Pick<Document, 'querySelector'> = document): string | null {
  const meta = doc.querySelector<HTMLMetaElement>('meta[property=csp-nonce]')
  const nonce = meta?.nonce || meta?.getAttribute('nonce') || ''
  if (!nonce) return null
  setNonce(nonce)
  return nonce
}
