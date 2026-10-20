import React, { useMemo, useState } from 'react';
import { trackEvent } from '../lib/tracking';

type Status = 'idle' | 'submitting' | 'success' | 'error';

type FormCopy = {
  nameLabel: string;
  namePlaceholder: string;
  companyLabel: string;
  companyPlaceholder: string;
  optionalLabel: string;
  phoneLabel: string;
  phonePlaceholder: string;
  emailLabel: string;
  emailPlaceholder: string;
  messageLabel: string;
  messagePlaceholder: string;
  submitLabel: string;
  submittingLabel: string;
  successMessage: string;
  errorMessage: string;
  missingEndpointMessage: string;
};

export default function ContactForm({ form }: { form: FormCopy }) {
  const [status, setStatus] = useState<Status>('idle');
  const [message, setMessage] = useState('');
  const endpoint = useMemo(() => {
    const configured = import.meta.env.PUBLIC_LEADS_ENDPOINT || '';
    if (configured) return String(configured);
    const base = import.meta.env.PUBLIC_ADMIN_URL || '';
    return base ? `${String(base).replace(/\/$/, '')}/site-admin-api/leads` : '/site-admin-api/leads';
  }, []);

  async function onSubmit(event: React.SyntheticEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    if (!endpoint) {
      setStatus('error');
      setMessage(form.missingEndpointMessage);
      return;
    }
    const formData = new FormData(formElement);
    if (String(formData.get('website') || '').trim()) return;
    const params = new URLSearchParams(window.location.search);
    const name = String(formData.get('name') || '').trim();
    const companyName = String(formData.get('companyName') || '').trim();
    const phone = String(formData.get('phone') || '').trim();
    const email = String(formData.get('email') || '').trim();
    const messageBody = String(formData.get('message') || '').trim();
    if (!phone && !email) {
      setStatus('error');
      setMessage(form.errorMessage);
      return;
    }
    setStatus('submitting');
    setMessage('');
    try {
      const response = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          data: {
            name,
            companyName,
            contactInfo: [phone, email].filter(Boolean).join(' / '),
            phone,
            email,
            message: messageBody,
            sourcePage: window.location.pathname,
            sourceUrl: window.location.href,
            referrer: document.referrer,
            utm: {
              source: params.get('utm_source') || '',
              medium: params.get('utm_medium') || '',
              campaign: params.get('utm_campaign') || '',
              term: params.get('utm_term') || '',
              content: params.get('utm_content') || ''
            },
            submittedAt: new Date().toISOString()
          }
        })
      });
      if (!response.ok) throw new Error('submit failed');
      formElement.reset();
      setStatus('success');
      setMessage(form.successMessage);
      // Consent-gated first-party analytics (no-op until analytics is allowed).
      trackEvent('lead_submit', { path: window.location.pathname, referrer: document.referrer || '' });
    } catch {
      setStatus('error');
      setMessage(form.errorMessage);
    }
  }

  return (
    <form className="space-y-6" onSubmit={onSubmit}>
      <input name="website" className="hidden" tabIndex={-1} autoComplete="off" />
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
        <div>
          <label htmlFor="contact-name" className="mb-2 block text-sm font-semibold text-gray-700">{form.nameLabel}</label>
          <input id="contact-name" name="name" type="text" required className="w-full rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 transition-colors focus:border-brand-500 focus:outline-none" placeholder={form.namePlaceholder} />
        </div>
        <div>
          <label htmlFor="contact-company" className="mb-2 block text-sm font-semibold text-gray-700">{form.companyLabel} <span className="font-normal text-gray-400">({form.optionalLabel})</span></label>
          <input id="contact-company" name="companyName" type="text" className="w-full rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 transition-colors focus:border-brand-500 focus:outline-none" placeholder={form.companyPlaceholder} />
        </div>
        <div>
          <label htmlFor="contact-phone" className="mb-2 block text-sm font-semibold text-gray-700">{form.phoneLabel}</label>
          <input id="contact-phone" name="phone" type="tel" className="w-full rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 transition-colors focus:border-brand-500 focus:outline-none" placeholder={form.phonePlaceholder} />
        </div>
        <div>
          <label htmlFor="contact-email" className="mb-2 block text-sm font-semibold text-gray-700">{form.emailLabel} <span className="font-normal text-gray-400">({form.optionalLabel})</span></label>
          <input id="contact-email" name="email" type="email" className="w-full rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 transition-colors focus:border-brand-500 focus:outline-none" placeholder={form.emailPlaceholder} />
        </div>
      </div>
      <div>
        <label htmlFor="contact-message" className="mb-2 block text-sm font-semibold text-gray-700">{form.messageLabel} <span className="font-normal text-gray-400">({form.optionalLabel})</span></label>
        <textarea id="contact-message" name="message" className="min-h-28 w-full rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 transition-colors focus:border-brand-500 focus:outline-none" placeholder={form.messagePlaceholder} />
      </div>
      {message && <p className={`text-sm ${status === 'success' ? 'text-green-600' : 'text-red-600'}`}>{message}</p>}
      <button type="submit" disabled={status === 'submitting'} className="w-full bg-brand-500 hover:bg-brand-700 disabled:opacity-60 text-white font-bold py-4 rounded-xl transition-all shadow-md hover:shadow-lg">
        {status === 'submitting' ? form.submittingLabel : form.submitLabel}
      </button>
    </form>
  );
}
