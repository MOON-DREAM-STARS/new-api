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
import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ModelOrbit } from '../model-orbit'

export function ModelEcosystem() {
  const { t } = useTranslation()

  return (
    <section id='model-ecosystem' className='dreamstars-model-section'>
      <div className='dreamstars-model-heading'>
        <div>
          <span className='dreamstars-kicker'>{t('Model ecosystem')}</span>
          <h2>{t('Mainstream models, all in one place')}</h2>
          <p>
            {t(
              'Covering leading closed and open model ecosystems worldwide, with more capabilities continuously being added.'
            )}
          </p>
        </div>
        <div className='dreamstars-discount'>
          <span>{t('Best available discount')}</span>
          <strong>
            {t('As low as 0.1× official price')}
            <em>{t('That is 90% off')}</em>
          </strong>
          <p>{t('Actual discounts are subject to the model catalog labels')}</p>
        </div>
      </div>

      <ModelOrbit />

      <div className='dreamstars-model-cta'>
        <Link to='/pricing'>
          {t('View the complete model catalog')}
          <ArrowRight aria-hidden='true' />
        </Link>
        <p>
          {t(
            'Specific available models, versions, and discounts are subject to real-time information on the model catalog page.'
          )}
        </p>
      </div>
    </section>
  )
}
