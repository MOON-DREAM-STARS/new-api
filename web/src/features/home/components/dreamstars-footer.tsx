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
import { useTranslation } from 'react-i18next'

interface DreamstarsFooterProps {
  docsUrl: string
}

export function DreamstarsFooter(props: DreamstarsFooterProps) {
  const { t } = useTranslation()
  const docsIsExternal = props.docsUrl.startsWith('http')

  return (
    <footer className='dreamstars-footer'>
      <div className='dreamstars-footer-inner'>
        <div className='dreamstars-footer-brand'>
          <Link to='/'>{t('Dream Relay Station')}</Link>
          <p>
            {t(
              'An AI model aggregation and transparent billing platform for university faculty and students'
            )}
          </p>
          <span>{t('© 2026 Dream Relay Station')}</span>
        </div>

        <div className='dreamstars-footer-meta'>
          <nav aria-label={t('Footer navigation')}>
            <Link to='/pricing'>{t('Models and Pricing')}</Link>
            <a href='/#getting-started'>{t('Use Guide')}</a>
            {docsIsExternal ? (
              <a href={props.docsUrl} target='_blank' rel='noopener noreferrer'>
                {t('Docs')}
              </a>
            ) : (
              <Link to={props.docsUrl}>{t('Docs')}</Link>
            )}
            <Link to='/privacy-policy'>{t('Data Security')}</Link>
            <Link to='/about'>{t('About')}</Link>
          </nav>
          <p>
            {t('Built on')}{' '}
            <a
              href='https://github.com/QuantumNous/new-api'
              target='_blank'
              rel='noopener noreferrer'
            >
              New API
            </a>
          </p>
        </div>
      </div>
    </footer>
  )
}
