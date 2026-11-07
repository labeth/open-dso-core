// ENGMODEL-OWNER-UNIT: FU-APP-WEB
// Run by TestMathCalibrationJS (math_calibration_node_test.go).
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const text=fs.readFileSync('app_draw.js','utf8');
const code=text.slice(text.indexOf('function mathParse('),text.indexOf('function drawRefTrace'));
const c={frame:{c1:[103,153],c2:[103,128],vpc1:.04,vpc2:.08,off1_v:-1,off2_v:-2},st:{},mathMemo:{},fftCh:{},mathFn:''};
vm.createContext(c);vm.runInContext(fs.readFileSync('caps.js','utf8'),c);vm.runInContext(code,c);
for(const [mode,want] of [['c1+c2',[128,228]],['c1-c2',[128,128]],['c2-c1',[128,128]],['c1*c2',[128,140.5]]]){
 c.mathFn=mode;assert.deepEqual(Array.from(c.computeMath()),want,mode);
}
c.mathFn='c1-c2';c.frame={c1:[153],c2:[153],vpc1:.08,vpc2:.08};c.st={zoom1:2};
assert.deepEqual(Array.from(c.computeMath()),[128],'equal tip volts despite zoom/probe');
c.frame={...c.frame,off1_v:1};assert.deepEqual(Array.from(c.computeMath()),[103],'scale/offset change invalidates cached result');
c.st.zoom1=1;assert.deepEqual(Array.from(c.computeMath()),[115.5],'stopped zoom change invalidates cached result');
c.frame={c1:[128,140],c2:[128],vpc1:.04,vpc2:.04};
assert.deepEqual(Array.from(c.computeMath()),[128,-1],'missing samples remain gaps');
// four channels of 12-bit codes in the display domain (Float32, fractional
// units): any pair, C3's V/div the scale of C3 − C4
c.st={};
c.frame={c1:[128,128],c2:[128,128],c3:Float32Array.from([128.5,153.25]),c4:Float32Array.from([128,128]),vpc1:.04,vpc2:.04,vpc3:.02,vpc4:.04,off3_v:0,off4_v:0.5};
c.mathFn='c3-c4';assert.deepEqual(Array.from(c.computeMath()).map((v)=>+v.toFixed(4)),[153.5,178.25],'C3 − C4 in volts at C3\'s scale');
c.mathFn='c4-c3';assert.deepEqual(Array.from(c.computeMath()).map((v)=>+v.toFixed(4)),[102.5,77.75],'C4 − C3 at C3\'s scale');
c.mathFn='c3*c4';assert.equal(c.computeMath().length,2,'a product of C3 and C4');
c.mathFn='c4-c5';assert.equal(c.computeMath(),null,'a channel the frame does not carry: no trace');
c.mathFn='res3';c.fftCh={3:{sel:[]}};assert.equal(c.computeMath(),null,'C3 less no picked peaks: no trace');
assert.equal(JSON.stringify(c.mathParse('c2-c11')),'{"op":"-","a":2,"b":11}');assert.equal(c.mathParse('off'),null);
console.log('ALL PASS');
