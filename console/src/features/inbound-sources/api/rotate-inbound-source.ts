import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { RotateInboundSourceSecretResponse } from '@/proto/attune/v1/inbound_source'

// useRotateInboundSource — webhook-only. Rotates the HMAC secret with a
// 24h dual-secret grace window; the server returns 409
// rotation_in_grace_window if called again before that window closes.
// The fresh secret_hex is returned ONCE; surface it immediately.
export function useRotateInboundSource() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<RotateInboundSourceSecretResponse>(`/fb/v1/console/inbound/sources/${id}/rotate-secret`, {
        method: 'POST',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'inbound-sources'] })
    },
  })
}
