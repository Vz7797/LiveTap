import { QueryClient } from "@tanstack/react-query";

export const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false, staleTime: 2500 } } });
let csrf = "";
export const setCSRF = (value: string) => { csrf = value; };
export class ApiError extends Error { constructor(message: string, public code: string, public status: number) { super(message); } }
export async function api<T>(path: string, method = "GET", body?: unknown): Promise<T> {
  const response = await fetch(`/admin/api${path}`, { method, credentials: "same-origin", headers: { "Content-Type": "application/json", ...(method === "GET" ? {} : { "X-CSRF-Token": csrf }) }, body: body === undefined ? undefined : JSON.stringify(body) });
  const value = await response.json();
  if (!response.ok) {
    if (response.status === 401 && path !== "/login") void queryClient.invalidateQueries({ queryKey: ["session"] });
    throw new ApiError(value.error?.message ?? "请求失败", value.error?.code ?? "unknown", response.status);
  }
  return value;
}
export async function logout() { await api("/logout", "POST"); window.location.reload(); }
