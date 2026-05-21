import { ChevronDown } from 'lucide-react';
import React, { useState } from 'react';
import type { FAQ } from '../lib/types';

export default function FAQAccordion({ faqs }: { faqs: FAQ[] }) {
  const [openIndex, setOpenIndex] = useState(0);

  return (
    <div className="space-y-4">
      {faqs.map((faq, index) => (
        <div key={faq.id} className="bg-white border border-gray-200 rounded-2xl overflow-hidden shadow-sm">
          <button
            onClick={() => setOpenIndex(openIndex === index ? -1 : index)}
            className="w-full text-left px-8 py-6 flex justify-between items-center bg-white hover:bg-gray-50 transition-colors gap-4"
          >
            <span className="font-bold text-lg text-gray-800">{faq.question}</span>
            <ChevronDown className={`text-gray-400 transition-transform shrink-0 ${openIndex === index ? 'rotate-180' : ''}`} />
          </button>
          {openIndex === index && (
            <div className="px-8 pb-6 text-gray-600 leading-relaxed border-t border-gray-100 bg-gray-50/50 pt-4">
              {faq.answer}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
