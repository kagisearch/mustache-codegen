const esc=(e)=>{var a=""+e,t=match_html.exec(a);if(!t)return e;var r,c,n,s="";for(r=t.index,c=0;r<a.length;r++){switch(a.charCodeAt(r)){case 34:n="&quot;";break;case 38:n="&amp;";break;case 60:n="&lt;";break;case 62:n="&gt;";break;default:continue}c!==r&&(s+=a.substring(c,r)),c=r+1,s+=n}return c!==r?s+a.substring(c,r):s}
const match_html = /["&<>]/
const look=(s,k)=>{for(let i=s.length-1;i>=0;i--){if(Object.prototype.hasOwnProperty.call(s[i], k))return s[i][k]}return undefined}
const arr=Array.isArray
const f=(x)=>!x||(arr(x)&&x.length===0)
