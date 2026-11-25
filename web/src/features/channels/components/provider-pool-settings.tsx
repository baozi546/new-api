import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'

import { channelsQueryKeys } from '../lib'
import {
  poolQueryKeys,
  saveProviderPool,
  type PoolSettings,
} from '../lib/provider-pool-api'

const settingsSchema = z.object({
  concurrency: z.number().int().min(1).max(10000),
})

export function ProviderPoolSettings(props: {
  channelId: number
  settings: PoolSettings
  disabled: boolean
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<PoolSettings>({
    resolver: zodResolver(settingsSchema),
    defaultValues: props.settings,
  })
  const mutation = useMutation({
    mutationFn: (values: PoolSettings) =>
      saveProviderPool(props.channelId, values),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['provider-account-pool'] }),
        queryClient.invalidateQueries({ queryKey: poolQueryKeys.channels }),
        queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() }),
        queryClient.invalidateQueries({
          queryKey: channelsQueryKeys.detail(props.channelId),
        }),
      ])
    },
  })
  return (
    <div className='rounded-lg border p-4'>
      <Field data-invalid={!!form.formState.errors.concurrency}>
        <FieldLabel htmlFor={`provider-pool-concurrency-${props.channelId}`}>
          {t('Concurrency per account')}
        </FieldLabel>
        <Input
          id={`provider-pool-concurrency-${props.channelId}`}
          type='number'
          min={1}
          max={10000}
          disabled={props.disabled || mutation.isPending}
          {...form.register('concurrency', { valueAsNumber: true })}
          aria-invalid={!!form.formState.errors.concurrency}
        />
        {form.formState.errors.concurrency && (
          <FieldError>{t('Enter a number from 1 to 10000')}</FieldError>
        )}
      </Field>
      <FieldDescription className='my-3'>
        {t('Requests above this channel limit wait for an account slot.')}
      </FieldDescription>
      <Button
        type='button'
        size='sm'
        disabled={
          props.disabled || mutation.isPending || !form.formState.isDirty
        }
        onClick={() =>
          void form.handleSubmit((values) => mutation.mutate(values))()
        }
      >
        {t('Save account pool settings')}
      </Button>
    </div>
  )
}
