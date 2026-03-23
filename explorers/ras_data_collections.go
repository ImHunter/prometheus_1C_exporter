package exporter

import (
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Структурированное хранение данных сессии, прочитанных из rac
type labeledValues struct {
	// Значения идентификаторов (меток)
	labelsData map[string]string
	// Значения счетчиков ("memorytotal" и т.п.)
	metersData map[string]*int64
}

// Конструктор для labeledValues
func newLabeledValues() *labeledValues {
	sd := labeledValues{
		labelsData: make(map[string]string),
		metersData: make(map[string]*int64),
	}
	return &sd
}

func (data *labeledValues) GetWithAll() prometheus.Labels {
	names := []string{}
	for k := range data.labelsData {
		names = append(names, k)
	}
	return data.GetWith(names...)
}

func (data *labeledValues) GetWith(names ...string) prometheus.Labels {
	getWith := make(prometheus.Labels)
	for _, lb := range names {
		getWith[lb] = data.labelsData[lb]
	}
	return getWith
}

func (lv *labeledValues) readMeterValues(rasRowItem *map[string]string, meterParams MeterParamsCollection) {
	var readedVal *int64
	for _, mp := range meterParams {
		readedVal = mp.readValue(rasRowItem)
		if readedVal != nil {
			lv.metersData[mp.Name] = readedVal
		}
	}
}

func (lv *labeledValues) applyToCollection(buff *labeledValuesCollection, meterParams MeterParamsCollection) {

	buff.mu.Lock()
	defer buff.mu.Unlock()

	var readedVal *int64
	var existingVal *int64

	buffKey, err := buff.createKey(lv)
	if err != nil {
		// Пока не решил окончательно, нужна ли паника. Или может просто возврат.
		panic(fmt.Sprintf("%v", err))
	}

	bufferData := buff.data()[buffKey]
	if bufferData == nil {
		buff.dataMap[buffKey] = lv
		bufferData = lv
	} else {
		for _, mp := range meterParams {
			existingVal = bufferData.metersData[mp.Name]
			readedVal = lv.metersData[mp.Name]
			v, a := mp.applyValue(readedVal, existingVal)
			if a {
				bufferData.metersData[mp.Name] = v
			}
		}
		lv.clear()
	}
}

func (lv *labeledValues) getLabelValues(labelNames ...string) []string {
	retVal := make([]string, 0)
	for _, v := range labelNames {
		retVal = append(retVal, lv.labelsData[v])
	}
	return retVal
}

func (lv *labeledValues) clear() {
	for k := range lv.labelsData {
		delete(lv.labelsData, k)
	}
	for k := range lv.metersData {
		delete(lv.metersData, k)
	}
}

// Ключ для буфера данных, может состоять из нескольких элементов (до 3-х полей)
type labeledValuesMapKey [3]string
type labeledValuesMap map[labeledValuesMapKey]*labeledValues
type labeledValuesCollection struct {
	mu        sync.RWMutex
	dataMap   labeledValuesMap
	keyFields []string
}

func newLabeledValuesCollection(keys ...string) labeledValuesCollection {
	vc := labeledValuesCollection{
		dataMap:   labeledValuesMap{},
		keyFields: keys,
	}
	return vc
}

func (buff *labeledValuesCollection) data() labeledValuesMap {
	return buff.dataMap
}

func (buff *labeledValuesCollection) deleteByKey(key labeledValuesMapKey) {
	lv := buff.dataMap[key]
	if lv != nil {
		lv.clear()
	}
	buff.dataMap[key] = nil
	delete(buff.dataMap, key)
}

func (buff *labeledValuesCollection) get(key labeledValuesMapKey) (*labeledValues, bool) {
	buff.mu.RLock()
	defer buff.mu.RUnlock()
	val, ok := buff.dataMap[key]
	return val, ok
}

func (buff *labeledValuesCollection) set(key labeledValuesMapKey, lv *labeledValues) {
	buff.mu.Lock()
	defer buff.mu.Unlock()
	if lv != nil {
		buff.dataMap[key] = lv
	} else if _, ok := buff.get(key); ok {
		buff.deleteByKey(key)
	}
}

func (buff *labeledValuesCollection) clear() {
	for k, _ := range buff.dataMap {
		buff.deleteByKey(k)
	}
}

func (buff *labeledValuesCollection) createKey(lv *labeledValues) (labeledValuesMapKey, error) {
	var retVal labeledValuesMapKey
	var keyVal string
	var keyExists bool
	for k, v := range buff.keyFields {
		keyVal, keyExists = lv.labelsData[v]
		if keyExists {
			retVal[k] = keyVal
		} else {
			return labeledValuesMapKey{}, fmt.Errorf("значение ключа %q не найдено в значениях меток", v)
		}
	}
	return retVal, nil
}

type ExemplarChecker struct {
	data *labeledValuesCollection
	keys map[labeledValuesMapKey]map[string]bool
}

func newExemplarChecker(buff *labeledValuesCollection) ExemplarChecker {
	ch := ExemplarChecker{
		data: buff,
		keys: make(map[labeledValuesMapKey]map[string]bool),
	}
	return ch
}

func (finder *ExemplarChecker) isExemplar(lv *labeledValues, paramName string) bool {
	key, err := finder.data.createKey(lv)
	if err != nil {
		return false
	}
	is := finder.keys[key][paramName]
	return is
}

// В качестве экземпляров определяет случайные.
//
// Parameters:
//
//	percent - Сколько процентов данных нужно поместить в экземпляры.
func (finder *ExemplarChecker) findRandomExemplars(percent int) {
	gen := rand.New(rand.NewSource(time.Now().UnixNano()))
	for key, lv := range finder.data.data() {
		for p, _ := range lv.metersData {
			if gen.Intn(100) > percent {
				continue
			}
			parMap := finder.keys[key]
			if parMap == nil {
				parMap = make(map[string]bool)
			}
			finder.keys[key] = parMap
			parMap[p] = true
		}
	}
}

func (finder *ExemplarChecker) clear() {
	clear(finder.keys)
}

func atoi(n string) *int64 {
	if v, err := strconv.ParseInt(n, 10, 64); err == nil {
		return &v
	}
	return nil
}

func parseTime(txt string) (time.Time, error) {

	t, err := time.ParseInLocation("2006-01-02T15:04:05", txt, localTimeLocation)
	if err == nil {
		return t, nil
	}

	return time.Time{}, err
}
