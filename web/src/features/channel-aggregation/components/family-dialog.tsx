/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import z from 'zod'

import { Dialog } from '@/components/dialog'
import { TagInput } from '@/components/tag-input'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { handleServerError } from '@/lib/handle-server-error'

import { createFamily, updateFamily } from '../api'
import type { ModelFamily } from '../types'

const familySchema = z.object({
  key: z
    .string()
    .trim()
    .regex(/^[a-z0-9][a-z0-9_-]{0,63}$/, {
      message:
        'The family key may contain only lowercase letters, digits, hyphens and underscores',
    }),
  name: z
    .string()
    .trim()
    .min(1, { message: 'The family name must be 1 to 128 characters' })
    .max(128, {
      message: 'The family name must be 1 to 128 characters',
    }),
  description: z
    .string()
    .max(255, { message: 'The description must be at most 255 characters' }),
  sort_order: z.number().int(),
  match_patterns: z
    .array(z.string())
    .max(20, { message: 'A family can have at most 20 match patterns' }),
})

type FamilyFormValues = z.infer<typeof familySchema>

type FamilyDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  family?: ModelFamily
  nextSortOrder: number
  onSaved: (familyId: number) => void
}

export function FamilyDialog(props: FamilyDialogProps) {
  const { t } = useTranslation()
  const editing = Boolean(props.family)
  const form = useForm<FamilyFormValues>({
    resolver: zodResolver(familySchema),
    defaultValues: {
      key: '',
      name: '',
      description: '',
      sort_order: 0,
      match_patterns: [],
    },
  })
  useEffect(() => {
    if (!props.open) return
    form.reset(
      props.family
        ? {
            key: props.family.key,
            name: props.family.name,
            description: props.family.description,
            sort_order: props.family.sort_order,
            match_patterns: props.family.match_patterns,
          }
        : {
            key: '',
            name: '',
            description: '',
            sort_order: props.nextSortOrder,
            match_patterns: [],
          }
    )
  }, [props.open, props.family, props.nextSortOrder, form])

  const save = useMutation({
    mutationFn: async (values: FamilyFormValues) => {
      const input = { ...values, version: props.family?.version ?? 0 }
      if (props.family) {
        await updateFamily(props.family.id, input)
        return props.family.id
      }
      return (await createFamily(input)).id
    },
    onSuccess: (id) => {
      toast.success(
        editing ? t('Model family updated') : t('Model family created')
      )
      props.onOpenChange(false)
      props.onSaved(id)
    },
    onError: (error) => handleServerError(error, t('Failed to save')),
  })

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={editing ? t('Edit model family') : t('New model family')}
      description={t(
        'A family groups related models so they share tiers and channel pools.'
      )}
      contentClassName='sm:max-w-lg'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={save.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='model-family-form'
            disabled={save.isPending}
          >
            {save.isPending && <Spinner data-icon='inline-start' />}
            {t('Save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id='model-family-form'
          className='space-y-4'
          onSubmit={form.handleSubmit((values) => save.mutate(values))}
        >
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Name')}</FormLabel>
                <FormControl>
                  <Input {...field} placeholder='DeepSeek' />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='key'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Key')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    disabled={editing}
                    placeholder='deepseek'
                    className='font-mono'
                  />
                </FormControl>
                <FormDescription>
                  {t('A stable identifier. It cannot be changed later.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='description'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Description')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='match_patterns'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Name hints')}</FormLabel>
                <FormControl>
                  <TagInput
                    value={field.value}
                    onChange={field.onChange}
                    placeholder={t('Type a fragment and press Enter')}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Models whose names contain a hint are suggested when you manage the family. Membership is always chosen by hand.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='sort_order'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Sort order')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    value={field.value}
                    onChange={(event) =>
                      field.onChange(Number(event.target.value) || 0)
                    }
                    className='w-32'
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
