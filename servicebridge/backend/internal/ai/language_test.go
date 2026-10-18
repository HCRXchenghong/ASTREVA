package ai

import "testing"

func TestDetectLanguageByScript(t *testing.T) {
	cases := []struct {
		text     string
		code     string
		dialect  string
		minScore float64
	}{
		{"こんにちは、製品の価格を教えてください。", LangJa, "", 0.3},
		{"안녕하세요, 제품 가격을 알려주세요.", LangKo, "", 0.3},
		{"Здравствуйте, подскажите цену на продукт.", LangRu, "", 0.3},
		{"สวัสดีครับ ราคาเท่าไหร่", LangTh, "", 0.3},
		{"שלום, מה המחיר?", LangHe, "", 0.3},
		{"Γεια σας, ποια είναι η τιμή;", LangEl, "", 0.3},
		{"नमस्ते, कीमत क्या है?", LangHi, "", 0.3},
		{"مرحبا، شلون أقدر أشتري المنتج؟", LangAr, "ar-Gulf", 0.3},
		{"ازيك، عايز أعرف السعر", LangAr, "ar-EG", 0.3},
		{"كيفك، بدي أعرف السعر منيح", LangAr, "ar-LEV", 0.3},
		{"بزاف زوين، واش كاين الثمن؟", LangAr, "ar-MA", 0.3},
		{"你好，请问这个产品的价格是多少？", LangZh, "zh-Hans", 0.3},
		{"這個產品的價格是多少？請問你們怎麼賣？", LangZh, "zh-Hant", 0.3},
		{"唔該，呢個產品幾錢？", LangYue, "yue-Hant", 0.3},
	}
	for _, item := range cases {
		result := DetectLanguage(item.text)
		if result.Code != item.code {
			t.Errorf("DetectLanguage(%q) code = %q, want %q", item.text, result.Code, item.code)
		}
		if item.dialect != "" && result.Dialect != item.dialect {
			t.Errorf("DetectLanguage(%q) dialect = %q, want %q", item.text, result.Dialect, item.dialect)
		}
		if result.Confidence < item.minScore {
			t.Errorf("DetectLanguage(%q) confidence = %v, want >= %v", item.text, result.Confidence, item.minScore)
		}
	}
}

func TestDetectLanguageLatin(t *testing.T) {
	cases := []struct {
		text    string
		code    string
		dialect string
	}{
		{"Hello, could you please tell me the price of this product?", LangEn, ""},
		{"We would like to organise delivery, the colour should be grey.", LangEn, "en-GB"},
		{"Hola, ¿cuál es el precio de este producto? Gracias.", LangEs, ""},
		{"Buenos días, ¿me podéis decir el precio? Vale, gracias.", LangEs, "es-ES"},
		{"Hola, necesito una cotización, ¿me la mandas por carro? Platicamos ahorita.", LangEs, "es-419"},
		{"Bonjour, quel est le prix de ce produit ? Merci beaucoup.", LangFr, ""},
		{"Guten Tag, wie ist der Preis für dieses Produkt? Bitte danke.", LangDe, ""},
		{"Olá, qual é o preço deste produto? Obrigado, você pode enviar por celular?", LangPt, "pt-BR"},
		{"Buongiorno, vorrei sapere il prezzo, grazie mille.", LangIt, ""},
		{"Xin chào, giá của sản phẩm này là bao nhiêu? Cảm ơn bạn.", LangVi, ""},
		{"Merhaba, bu ürünün fiyatı ne kadar? Teşekkür ederim.", LangTr, ""},
		{"Halo, berapa harga produk ini? Terima kasih.", LangID, ""},
		{"Dzień dobry, jaka jest cena tego produktu? Dziękuję.", LangPl, ""},
		{"Bună ziua, care este prețul acestui produs? Mulțumesc.", LangRo, ""},
		{"Goedemorgen, wat is de prijs van dit product? Dank u.", LangNl, ""},
	}
	for _, item := range cases {
		result := DetectLanguage(item.text)
		if result.Code != item.code {
			t.Errorf("DetectLanguage(%q) = %q (confidence %v, candidates %v), want %q",
				item.text, result.Code, result.Confidence, result.Candidates, item.code)
		}
		if item.dialect != "" && result.Dialect != item.dialect {
			t.Errorf("DetectLanguage(%q) dialect = %q, want %q", item.text, result.Dialect, item.dialect)
		}
	}
}

func TestDetectLangCompatWrapper(t *testing.T) {
	if got := DetectLang("Hola, gracias por el precio"); got != LangEs {
		t.Errorf("DetectLang(spanish) = %q, want %q", got, LangEs)
	}
	if got := DetectLang("你好"); got != LangZh {
		t.Errorf("DetectLang(chinese) = %q, want %q", got, LangZh)
	}
	if got := DetectLang(""); got != LangZh {
		t.Errorf("DetectLang(empty) = %q, want %q", got, LangZh)
	}
}

func TestNormalizeLang(t *testing.T) {
	cases := map[string]string{
		"es-MX":    LangEs,
		"es_419":   LangEs,
		"zh-Hant":  LangZh,
		"zh-CN":    LangZh,
		"yue":      LangZh,
		"ar-Gulf":  LangAr,
		"en-US":    LangEn,
		"pt-BR":    LangPt,
		"Russian":  LangRu,
		"spanish":  LangEs,
		"英语":       LangEn,
		"":         LangZh,
		"unknown!": LangZh,
	}
	for input, want := range cases {
		if got := NormalizeLang(input, LangZh); got != want {
			t.Errorf("NormalizeLang(%q) = %q, want %q", input, got, want)
		}
	}
	if got := NormalizeLang("", LangAr); got != LangAr {
		t.Errorf("NormalizeLang(empty, fallback=ar) = %q, want %q", got, LangAr)
	}
}

func TestLangName(t *testing.T) {
	cases := map[string]string{
		LangEs:   "Spanish",
		LangPt:   "Portuguese",
		LangJa:   "Japanese",
		LangZh:   "Simplified Chinese",
		LangAr:   "Arabic",
		"es-419": "Spanish",
	}
	for input, want := range cases {
		if got := LangName(input); got != want {
			t.Errorf("LangName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseLanguageInstruction(t *testing.T) {
	positive := []struct {
		text string
		code string
	}{
		{"请用英语回答我", LangEn},
		{"能不能用西班牙语回复我？", LangEs},
		{"用阿拉伯语回答", LangAr},
		{"请以日语回复", LangJa},
		{"Please reply in Spanish", LangEs},
		{"could you answer in French please", LangFr},
		{"responde en portugués por favor", LangPt},
		{"répondez en allemand s'il vous plaît", LangDe},
		{"bitte antworte auf Russisch", LangRu},
		{"отвечай на русском", LangRu},
		{"日本語で答えてください", LangJa},
		{"한국어로 답변해주세요", LangKo},
		{"من فضلك بالعربية", LangAr},
	}
	for _, item := range positive {
		code, ok := ParseLanguageInstruction(item.text)
		if !ok || code != item.code {
			t.Errorf("ParseLanguageInstruction(%q) = (%q, %v), want (%q, true)", item.text, code, ok, item.code)
		}
	}
	negative := []string{
		"请回答我的问题",
		"这个产品的价格是多少？",
		"Hello, I have a question about delivery",
		"¿Cuánto cuesta?",
		"",
	}
	for _, text := range negative {
		if code, ok := ParseLanguageInstruction(text); ok {
			t.Errorf("ParseLanguageInstruction(%q) unexpectedly matched %q", text, code)
		}
	}
}

func TestDetectDominantLanguage(t *testing.T) {
	if got := DetectDominantLanguage([]string{LangEs, LangEs, LangEn}, LangZh, 0.4); got != LangEs {
		t.Errorf("dominant = %q, want %q", got, LangEs)
	}
	// 平局取最近一条
	if got := DetectDominantLanguage([]string{LangEs, LangEn}, LangZh, 0.4); got != LangEn {
		t.Errorf("tie-break = %q, want %q (最近一条)", got, LangEn)
	}
	// 占比不足时回落初始语言
	if got := DetectDominantLanguage([]string{LangEs, LangEn, LangFr}, LangAr, 0.5); got != LangAr {
		t.Errorf("low-ratio fallback = %q, want %q", got, LangAr)
	}
	// 空列表回落初始语言
	if got := DetectDominantLanguage(nil, LangEn, 0.4); got != LangEn {
		t.Errorf("empty fallback = %q, want %q", got, LangEn)
	}
	// 只有一种语言时直接用
	if got := DetectDominantLanguage([]string{"es-MX", "es-ES", "es"}, LangZh, 0.4); got != LangEs {
		t.Errorf("single language = %q, want %q", got, LangEs)
	}
}
