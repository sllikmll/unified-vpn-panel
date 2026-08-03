import { useMutation, useQueryClient } from '@tanstack/react-query';

import { keys } from '@/api/queryKeys';
import type { ConfigProfileFormValues } from '@/schemas/api/config-profile';
import { HttpUtil } from '@/utils';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };

export function useConfigProfileMutations() {
  const queryClient = useQueryClient();
  const invalidate = () => queryClient.invalidateQueries({ queryKey: keys.profiles.root() });

  const createMut = useMutation({
    mutationFn: (payload: ConfigProfileFormValues) => HttpUtil.post('/panel/api/profiles/add', payload, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const updateMut = useMutation({
    mutationFn: ({ id, payload }: { id: number; payload: ConfigProfileFormValues }) =>
      HttpUtil.post(`/panel/api/profiles/update/${id}`, payload, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const cloneMut = useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) =>
      HttpUtil.post(`/panel/api/profiles/clone/${id}`, { name }, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const removeMut = useMutation({
    mutationFn: (id: number) => HttpUtil.post(`/panel/api/profiles/del/${id}`),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const setEnableMut = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      HttpUtil.post(`/panel/api/profiles/setEnable/${id}`, { enabled }, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  return {
    create: (payload: ConfigProfileFormValues) => createMut.mutateAsync(payload),
    update: (id: number, payload: ConfigProfileFormValues) => updateMut.mutateAsync({ id, payload }),
    clone: (id: number, name: string) => cloneMut.mutateAsync({ id, name }),
    remove: (id: number) => removeMut.mutateAsync(id),
    setEnable: (id: number, enabled: boolean) => setEnableMut.mutateAsync({ id, enabled }),
  };
}
