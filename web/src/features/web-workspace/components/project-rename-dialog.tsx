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
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'

import { useRenameWebWorkspaceProject } from '../hooks/use-web-workspace-projects'
import { classifyWebWorkspaceError, isPolicyDenied } from '../lib/errors'
import {
  renameProjectFormSchema,
  type RenameProjectFormValues,
} from '../lib/project-form'
import type { WebProject } from '../types'

type ProjectRenameDialogProps = {
  project: WebProject | null
  onOpenChange: (open: boolean) => void
  /** Called when the server reports the project as unavailable or removed. */
  onRemoved: (name: string) => void
}

function ProjectRenameForm(props: {
  project: WebProject
  onOpenChange: (open: boolean) => void
  onRemoved: (name: string) => void
}) {
  const { t } = useTranslation()
  const renameMutation = useRenameWebWorkspaceProject()
  const form = useForm<RenameProjectFormValues>({
    resolver: zodResolver(renameProjectFormSchema),
    defaultValues: { name: props.project.name },
  })

  const error = renameMutation.error
    ? classifyWebWorkspaceError(renameMutation.error)
    : null

  const handleSubmit = form.handleSubmit(async (values) => {
    try {
      await renameMutation.mutateAsync({
        id: props.project.id,
        name: values.name,
      })
      props.onOpenChange(false)
    } catch (failure) {
      // The optimistic list update is rolled back by the mutation; a policy
      // denial additionally marks the project as unavailable.
      if (isPolicyDenied(classifyWebWorkspaceError(failure))) {
        props.onRemoved(props.project.name)
      }
    }
  })

  return (
    <Form {...form}>
      <form
        noValidate
        onSubmit={(event) => {
          void handleSubmit(event)
        }}
        className='grid gap-4'
      >
        <FormField
          control={form.control}
          name='name'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Project name')}</FormLabel>
              <FormControl>
                <Input autoFocus maxLength={255} {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        {error ? (
          <Alert variant='destructive' role='alert'>
            <AlertTitle>
              {isPolicyDenied(error)
                ? t('Resource unavailable or removed')
                : t('Could not rename the project')}
            </AlertTitle>
            <AlertDescription>{t(error.messageKey)}</AlertDescription>
          </Alert>
        ) : null}
        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' disabled={renameMutation.isPending}>
            {renameMutation.isPending ? (
              <Spinner className='size-4 motion-reduce:animate-none' />
            ) : null}
            {t('Save changes')}
          </Button>
        </DialogFooter>
      </form>
    </Form>
  )
}

/** Inline rename dialog; the row list is updated optimistically and rolled back on failure. */
export function ProjectRenameDialog(props: ProjectRenameDialogProps) {
  const { t } = useTranslation()

  return (
    <Dialog open={Boolean(props.project)} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Rename project')}</DialogTitle>
          <DialogDescription>
            {t(
              'Only the registration shown here is renamed; the provider-side project keeps its name.'
            )}
          </DialogDescription>
        </DialogHeader>
        {props.project ? (
          <ProjectRenameForm
            project={props.project}
            onOpenChange={props.onOpenChange}
            onRemoved={props.onRemoved}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
