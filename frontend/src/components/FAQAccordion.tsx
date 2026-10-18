import { ChevronDown } from 'lucide-react';
import React, { useState } from 'react';
import type { FAQ } from '../lib/types';

export default function FAQAccordion({ faqs }: { faqs: FAQ[] }) {
  const [openIndex, setOpenIndex] = useState(0);

  return (
    <div className="space-y-4">
      {faqs.map((faq, index) => {
        const isOpen = openIndex === index;
        const questionId = `faq-question-${faq.id}`;
        const answerId = `faq-answer-${faq.id}`;
        return (
          <div key={faq.id} className="bg-white border border-gray-200 rounded-2xl overflow-hidden shadow-sm">
            <button
              type="button"
              id={questionId}
              aria-expanded={isOpen}
              aria-controls={answerId}
              onClick={() => setOpenIndex(isOpen ? -1 : index)}
              className="w-full text-left px-8 py-6 flex justify-between items-center bg-white hover:bg-gray-50 transition-colors gap-4"
            >
              <span className="font-bold text-lg text-gray-800">{faq.question}</span>
              <ChevronDown className={`text-gray-400 transition-transform shrink-0 ${isOpen ? 'rotate-180' : ''}`} />
            </button>
            <div
              id={answerId}
              role="region"
              aria-labelledby={questionId}
              hidden={!isOpen}
              className="px-8 pb-6 text-gray-600 leading-relaxed border-t border-gray-100 bg-gray-50/50 pt-4"
            >
              {faq.answer}
            </div>
          </div>
        );
      })}
    </div>
  );
}
