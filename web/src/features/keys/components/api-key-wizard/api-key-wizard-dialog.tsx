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
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Form } from '@/components/ui/form'
import { Spinner } from '@/components/ui/spinner'

import { useApiKeyWizard, WIZARD_STEP } from '../../hooks/use-api-key-wizard'
import type { ApiKey } from '../../types'
import { FamilyStep } from './family-step'
import { KeyResultStep } from './key-result-step'
import { OptionsStep } from './options-step'
import { TierStep } from './tier-step'
import { WizardSteps } from './wizard-steps'

type ApiKeyWizardProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The key being edited; none when creating keys. */
  currentRow?: ApiKey
}

function ApiKeyWizard(props: ApiKeyWizardProps) {
  const { t } = useTranslation()
  const wizard = useApiKeyWizard({
    open: props.open,
    currentRow: props.currentRow,
    onOpenChange: props.onOpenChange,
  })
  const step = wizard.step
  const busy = wizard.submitting
  const pageTopRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    // The dialog body scrolls, so every page opens at its top.
    pageTopRef.current?.scrollIntoView?.({ block: 'start' })
  }, [step])
  const lastEditableStep = WIZARD_STEP.options
  const stepLabels = [t('Model families'), t('Service tier'), t('Settings')]
  if (!wizard.isUpdate) stepLabels.push(t('API Key'))

  let stepValid = true
  if (step === WIZARD_STEP.families) stepValid = wizard.families.valid
  if (step === WIZARD_STEP.tier) stepValid = wizard.tier.valid

  let description = t('Choose which model families this API key can use')
  if (step === WIZARD_STEP.tier) {
    description = t('Choose the service tiers this API key uses')
  } else if (step === WIZARD_STEP.options) {
    description = t('Set the name, quota and expiration of the API key')
  } else if (step === WIZARD_STEP.key) {
    description = t('Your API key is ready')
  }

  let submitLabel = t('Create API Key')
  if (busy) submitLabel = t('Creating...')
  if (wizard.isUpdate) {
    submitLabel = busy ? t('Saving...') : t('Save changes')
  }

  const footer = (
    <>
      {step === WIZARD_STEP.families && (
        <Button variant='outline' onClick={() => props.onOpenChange(false)}>
          {t('Cancel')}
        </Button>
      )}
      {step > WIZARD_STEP.families && step <= lastEditableStep && (
        <Button
          variant='outline'
          disabled={busy}
          onClick={() => wizard.openStep(step - 1)}
        >
          {t('Back')}
        </Button>
      )}
      {step < lastEditableStep && (
        <Button disabled={!stepValid} onClick={() => wizard.openStep(step + 1)}>
          {t('Next')}
        </Button>
      )}
      {step === lastEditableStep && (
        <Button
          disabled={busy || !wizard.routingReady}
          onClick={() => void wizard.submit()}
        >
          {busy && <Spinner data-icon='inline-start' />}
          {submitLabel}
        </Button>
      )}
      {step === WIZARD_STEP.key && (
        <Button onClick={() => props.onOpenChange(false)}>{t('Done')}</Button>
      )}
    </>
  )

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => !busy && props.onOpenChange(open)}
      title={wizard.isUpdate ? t('Update API Key') : t('Create API Key')}
      description={description}
      contentHeight='min(62dvh, 600px)'
      showCloseButton={!busy}
      footer={footer}
    >
      <div ref={pageTopRef} aria-hidden />
      {step !== WIZARD_STEP.key && (
        <WizardSteps
          labels={stepLabels}
          current={step}
          onSelect={wizard.isUpdate ? wizard.openStep : undefined}
          isDisabled={(index) => busy || !wizard.canOpenStep(index)}
        />
      )}
      <div aria-busy={busy} inert={busy ? true : undefined}>
        {step === WIZARD_STEP.families && (
          <FamilyStep
            data={wizard.families.data}
            loading={wizard.families.loading}
            errorMessage={wizard.families.errorMessage}
            storedIds={wizard.families.storedIds}
            selection={wizard.families.selection}
            onChange={wizard.families.setSelection}
            onRetry={wizard.families.retry}
          />
        )}
        {step === WIZARD_STEP.tier && (
          <TierStep
            overview={wizard.tier.overview}
            loading={wizard.tier.loading}
            errorMessage={wizard.tier.errorMessage}
            own={wizard.tier.own}
            choice={wizard.tier.choice}
            onChoiceChange={wizard.tier.setChoice}
            onRetry={wizard.tier.retry}
          />
        )}
        {step === WIZARD_STEP.options && (
          <Form {...wizard.form}>
            <form
              id='api-key-form'
              onSubmit={(event) => {
                event.preventDefault()
                void wizard.submit()
              }}
            >
              <OptionsStep form={wizard.form} isUpdate={wizard.isUpdate} />
            </form>
          </Form>
        )}
        {step === WIZARD_STEP.key && wizard.created && (
          <KeyResultStep
            keys={wizard.created.keys}
            requested={wizard.created.requested}
            revealing={wizard.revealing}
            onRetryReveal={wizard.retryReveal}
          />
        )}
      </div>
    </Dialog>
  )
}

/**
 * Creates API keys, or edits one, in a centered window with up to four pages:
 * model families, service tier, settings and the new key.
 */
export function ApiKeyWizardDialog(props: ApiKeyWizardProps) {
  // Every opening starts with a clean wizard; the closing animation keeps the
  // old one until the dialog is gone.
  const [session, setSession] = useState(0)
  const [wasOpen, setWasOpen] = useState(props.open)
  if (props.open !== wasOpen) {
    setWasOpen(props.open)
    if (props.open) setSession((value) => value + 1)
  }
  return (
    <ApiKeyWizard
      key={`${session}:${props.currentRow?.id ?? 'new'}`}
      open={props.open}
      onOpenChange={props.onOpenChange}
      currentRow={props.currentRow}
    />
  )
}
