// Isolated browser tests never reach real task or model endpoints.
export async function get<T>(url: string): Promise<T> {
  const response = await fetch(url)
  if (!response.ok) throw { status: response.status }
  return response.json()
}
export async function post<T>(url: string, input: unknown): Promise<T> {
  const response = await fetch(url, { method: 'POST', body: JSON.stringify(input) })
  if (!response.ok) throw { status: response.status }
  return response.json()
}
