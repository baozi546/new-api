import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useId } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { channelsQueryKeys } from '../lib'
import {
  poolQueryKeys,
  saveProviderPool,
  type PoolSettings,
} from '../lib/provider-pool-api'

const settingsSchema = z.object({
  category: z.enum(['plus', 'pro', 'degraded', 'unknown']),
  concurrency: z.number().int().min(1).max(10000),
})

export function ProviderPoolSettings(props: {
  channelId: number
  settings: PoolSettings
  disabled: boolean
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const id = useId()
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
  const categories = [
    { value: 'plus', label: 'Plus' },
    { value: 'pro', label: 'Pro' },
    { value: 'degraded', label: 'Reduced quality' },
    { value: 'unknown', label: 'Unknown subscription' },
  ]
  return (
    <div className='rounded-lg border p-4'>
      <FieldGroup className='grid gap-3 sm:grid-cols-2'>
        <Field>
          <FieldLabel htmlFor={`${id}-category`}>
            {t('Account type used by this channel')}
          </FieldLabel>
          <Select
            value={form.watch('category')}
            items={categories.map((item) => ({
              value: item.value,
              label: t(item.label),
            }))}
            onValueChange={(value) => {
              const parsed = settingsSchema.shape.category.safeParse(value)
              if (parsed.success)
                {form.setValue('category', parsed.data, { shouldDirty: true })}
            }}
            disabled={props.disabled || mutation.isPending}
          >
            <SelectTrigger id={`${id}-category`} className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {categories.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {t(item.label)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field data-invalid={!!form.formState.errors.concurrency}>
          <FieldLabel htmlFor={`${id}-concurrency`}>
            {t('Concurrency per account')}
          </FieldLabel>
          <Input
            id={`${id}-concurrency`}
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
      </FieldGroup>
      <FieldDescription className='my-3'>
        {t(
          'Channels on the same provider and upstream address share accounts. Full accounts queue requests. Shared accounts obey the lowest applicable channel limit.'
        )}
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
