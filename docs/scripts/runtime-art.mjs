// Orthographic drawings for the four runtime choices. All annotations refer to
// objects in that runtime; the drawings use the same projection and pen weight.
const point = ([x, y, z]) => [212 + x * .96 - y * .8, 148 + x * .33 + y * .46 - z].map(n => +n.toFixed(2)).join(",");
const outline = (corners, face = "edge") => `<polygon class="${face}" points="${corners.map(point).join(" ")}"/>`;
const line = (points, kind = "detail") => `<polyline class="${kind}" points="${points.map(point).join(" ")}"/>`;

function block(x, y, z, w, d, h) {
  const X=x+w, Y=y+d, Z=z+h;
  return outline([[x,Y,z],[X,Y,z],[X,Y,Z],[x,Y,Z]], "front") +
    outline([[X,y,z],[X,Y,z],[X,Y,Z],[X,y,Z]], "side") +
    outline([[x,y,Z],[X,y,Z],[X,Y,Z],[x,Y,Z]], "top");
}

function container(x, y, z) {
  const w=114, d=48, h=43, X=x+w, Y=y+d, Z=z+h;
  let walls = block(x,y,z,w,d,h);
  for (let i=6; i<w-5; i+=7) {
    walls += line([[x+i,Y,z+4],[x+i,Y,Z-4],[x+i+2,Y,Z-4],[x+i+2,Y,z+4]], "rib");
    walls += line([[x+i,y+3,Z],[x+i,Y-3,Z]], "rib");
  }
  walls += line([[X,y+3,z+3],[X,Y-3,z+3],[X,Y-3,Z-3],[X,y+3,Z-3],[X,y+3,z+3]]);
  for (const offset of [12,24,36]) walls += line([[X,y+offset,z+4],[X,y+offset,Z-4]]);
  walls += line([[X,y+9,z+18],[X,y+15,z+18]], "accent") + line([[X,y+33,z+18],[X,y+39,z+18]], "accent");
  return walls;
}
function leader(points, x, y, label) {
  return `<polyline class="leader" pathLength="1" points="${points}"/><text class="legend" x="${x}" y="${y}">${label}</text>`;
}

function docker() {
  return block(-124,-57,-6,239,124,5) +
    container(-122,-47,0) + container(-4,-47,0) + container(-64,-45,43) +
    container(-102,7,0) +
    leader("284,71 309,48 343,48",312,42,"container") +
    leader("153,194 134,211 77,211",77,205,"Linux host");
}

function volume(x,y,z) {
  const [cx,cy]=point([x,y,z]).split(",").map(Number);
  return `<g transform="translate(${cx} ${cy})"><path class="front" d="M-23 0a23 9 0 0 0 46 0v-27h-46z"/>
    <ellipse class="top" cy="-27" rx="23" ry="9"/><path class="detail" d="M-23-9a23 9 0 0 0 46 0m-46-8a23 9 0 0 0 46 0"/></g>`;
}
function kubernetes() {
  const floor=outline([[-113,-68,0],[110,-68,0],[110,63,0],[-113,63,0]],"boundary");
  const pipes=line([[-63,-22,0],[-63,23,0],[67,23,0],[67,-29,0]],"route") +
    line([[5,-40,0],[5,23,0]],"route");
  let pods="";
  for (const [x,y] of [[-91,-51],[-12,-62],[-60,3]]) {
    pods+=block(x-4,y-4,0,42,40,5)+block(x,y,5,34,32,35);
    pods+=line([[x+4,y+4,40],[x+29,y+4,40],[x+29,y+27,40],[x+4,y+27,40],[x+4,y+4,40]]);
    pods+=line([[x+6,y+32,29],[x+24,y+32,29]],"accent");
  }
  return floor+pipes+pods+volume(66,-30,1)+
    leader("188,77 201,48 237,48",209,42,"Pod")+
    leader("311,150 331,169 363,169",334,163,"PVC");
}

function e2b() {
  let plates="";
  for (const z of [0,25,63]) {
    plates+=block(-88,-43,z,175,85,4);
    plates+=outline([[-80,-34,z+4],[79,-34,z+4],[79,34,z+4],[-80,34,z+4]],"boundary");
    for (let i=0;i<7;i++) {
      const x=-69+i*21;
      plates+=outline([[x,-18,z+4],[x+11,-18,z+4],[x+11,12,z+4],[x,12,z+4]],i===4?"memory":"top");
    }
    plates+=line([[-69,23,z+4],[14,23,z+4]],"detail");
  }
  return plates+leader("307,113 329,88 365,88",331,82,"memory")+
    leader("156,167 130,195 70,195",70,189,"snapshot");
}

function cloud() {
  let ports="";
  for (const x of [-98,-70,-42]) {
    ports+=block(x,-24,0,8,64,61);
    ports+=line([[x+8,-13,52],[x+8,29,52]],"accent");
  }
  return line([[-42,6,18],[10,6,18],[10,-42,18],[62,-42,18]],"route")+
    line([[-42,6,18],[10,6,18],[10,39,18],[62,39,18]],"route")+
    ports+block(51,-59,0,49,40,36)+block(51,22,0,49,40,36)+
    leader("286,124 305,81 347,81",313,75,"API")+
    leader("136,135 112,165 55,165",55,159,"gateway");
}

const drawings={docker, kubernetes, e2b, cloud};
export function runtimeArt(id) {
  return `<svg class="runtime-art" viewBox="0 0 425 235" aria-hidden="true" focusable="false">
    <g stroke-linecap="round" stroke-linejoin="round">${drawings[id]()}</g>
  </svg>`;
}
