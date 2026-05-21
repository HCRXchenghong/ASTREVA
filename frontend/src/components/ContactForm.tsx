import React, { useMemo, useState } from 'react';

type Status = 'idle' | 'submitting' | 'success' | 'error';

type FormCopy = {
  nameLabel: string;
  namePlaceholder: string;
  nameCompanyLabel?: string;
  nameCompanyPlaceholder?: string;
  companyLabel: string;
  companyPlaceholder: string;
  optionalLabel: string;
  phoneLabel: string;
  phonePlaceholder: string;
  contactLabel?: string;
  contactPlaceholder?: string;
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
    const base = import.meta.env.PUBLIC_ADMIN_URL || '';
    return base ? `${String(base).replace(/\/$/, '')}/site-admin-api/leads` : '';
  }, []);

  async function onSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!endpoint) {
      setStatus('error');
      setMessage(form.missingEndpointMessage);
      return;
    }
    const formData = new FormData(event.currentTarget);
    if (String(formData.get('website') || '').trim()) return;
    const params = new URLSearchParams(window.location.search);
    const nameCompany = String(formData.get('nameCompany') || '').trim();
    const contactInfo = String(formData.get('contactInfo') || '').trim();
    const looksLikeEmail = contactInfo.includes('@');
    setStatus('submitting');
    setMessage('');
    try {
      const response = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          data: {
            name: nameCompany,
            companyName: nameCompany,
            contactInfo,
            phone: looksLikeEmail ? '' : contactInfo,
            email: looksLikeEmail ? contactInfo : '',
            message: '',
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
      event.currentTarget.reset();
      setStatus('success');
      setMessage(form.successMessage);
    } catch {
      setStatus('error');
      setMessage(form.errorMessage);
    }
  }

  return (
    <form className="space-y-6" onSubmit={onSubmit}>
      <input name="website" className="hidden" tabIndex={-1} autoComplete="off" />
      <div>
        <label className="block text-sm font-semibold text-gray-700 mb-2">{form.nameCompanyLabel || '您的姓名 / 公司名'}</label>
        <input name="nameCompany" type="text" required className="w-full bg-gray-50 border border-gray-200 rounded-lg px-4 py-3 focus:outline-none focus:border-[#0a66c2] transition-colors" placeholder={form.nameCompanyPlaceholder || form.namePlaceholder} />
      </div>
      <div>
        <label className="block text-sm font-semibold text-gray-700 mb-2">{form.contactLabel || '手机号 / 邮箱号'}</label>
        <input name="contactInfo" type="text" required className="w-full bg-gray-50 border border-gray-200 rounded-lg px-4 py-3 focus:outline-none focus:border-[#0a66c2] transition-colors" placeholder={form.contactPlaceholder || form.phonePlaceholder} />
      </div>
      {message && <p className={`text-sm ${status === 'success' ? 'text-green-600' : 'text-red-600'}`}>{message}</p>}
      <button type="submit" disabled={status === 'submitting'} className="w-full bg-[#0a66c2] hover:bg-[#084b8c] disabled:opacity-60 text-white font-bold py-4 rounded-xl transition-all shadow-md hover:shadow-lg">
        {status === 'submitting' ? form.submittingLabel : form.submitLabel}
      </button>
    </form>
  );
}
