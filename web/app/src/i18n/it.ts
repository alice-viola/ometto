/**
 * Italiano. Typed against `en.ts`, so a key added there and forgotten here
 * fails the build.
 *
 * Where the domain is Italian to begin with the Italian word is the real one
 * and the English catalogue is the translation: the SAT/CAI grades are
 * Turistico, Escursionistico, Escursionisti Esperti and its attrezzata form,
 * a crag is a falesia, and a via ferrata was never anything else.
 */
type Messages = { [K in keyof typeof import('./en').default]: string };

const it: Messages = {
  // --- il documento ----------------------------------------------------------
  'meta.title': 'Ometto — percorsi in Trentino-Alto Adige',
  'meta.description':
    'Pianifica un percorso in Trentino-Alto Adige a piedi, in bici o in auto: tempi, dislivello, difficoltà del sentiero e profilo altimetrico.',
  'meta.shareTitle': 'Ometto — un percorso in Trentino-Alto Adige',
  'meta.region': 'Trentino-Alto Adige',

  // --- la cornice ------------------------------------------------------------
  'app.showPanel': 'Mostra il pannello',
  'app.showPanelTitle': 'Mostra il pannello (Esc)',
  'app.panel': 'Pannello',
  'app.offline': 'Il servizio di calcolo dei percorsi non è ancora raggiungibile.',

  // --- il pannello -----------------------------------------------------------
  'panel.title.plan': 'Ometto',
  'panel.title.history': 'Percorsi recenti',
  'panel.title.favourites': 'Preferiti',
  'panel.title.settings': 'Impostazioni',
  'panel.title.about': 'Informazioni',
  'panel.back': 'Torna al pianificatore',
  'panel.sections': 'Sezioni',
  'panel.lightTheme': 'Tema chiaro',
  'panel.darkTheme': 'Tema scuro',
  'panel.switchToLight': 'Passa al tema chiaro',
  'panel.switchToDark': 'Passa al tema scuro',
  'panel.hide': 'Nascondi il pannello',
  'panel.hideTitle': 'Nascondi il pannello (Esc)',
  'panel.mode': 'Mezzo',
  'panel.trailGrade': 'Difficoltà',
  'panel.useLifts': 'Usa gli impianti',
  'panel.useLiftsHint': 'Funivie e seggiovie, dove sono in funzione.',
  'panel.showThree': 'Mostra 3 percorsi',
  'panel.showThreeHint': "Confronta le alternative una accanto all'altra.",
  'panel.computing': 'Calcolo…',
  'panel.computingAria': 'Calcolo del percorso in corso',
  'panel.compute': 'Calcola percorso',
  'panel.needPointsClick': 'Imposta partenza e arrivo — oppure fai clic sulla mappa.',
  'panel.needPointsTap': 'Imposta partenza e arrivo — oppure tocca la mappa.',
  'panel.footer': 'Strumento di pianificazione, non una guida',
  'panel.footerData': 'Strumento di pianificazione, non una guida · dati {date}',
  'panel.aboutAria': 'Informazioni su Ometto, i suoi dati e i suoi limiti',
  'panel.aboutTitle': "Che cos'è, da dove vengono i dati e che cosa non può dirti",

  // --- i punti ---------------------------------------------------------------
  'search.points': 'Punti',
  'search.via': 'Passa per {n} punto | Passa per {n} punti',
  'search.clear': 'rimuovi',
  'search.clearAll': 'rimuovi tutto',
  'search.avoiding': 'Da evitare:',
  'search.stopAvoiding': 'Non evitare più {name}',
  'search.addStop': 'Aggiungi tappa',
  'search.swap': 'Inverti partenza e arrivo',
  'search.clearPoints': 'Cancella i punti, i passaggi e le strade evitate',
  'search.parkHint':
    'Aggiungi una tappa dove parcheggi: ci arrivi con il mezzo e da lì prosegui a piedi.',
  'search.addOne': 'Aggiungine una',
  'search.thatWay': 'quella strada',

  'point.from': 'Da',
  'point.to': 'A',
  'point.park': 'Dove parcheggi',
  'point.stop': 'Tappa {n}',
  'point.far': 'La strada o il sentiero più vicino dista {d}.',
  'point.starts': 'Il percorso parte a {d} da qui{where}.',
  'point.ends': 'Il percorso finisce a {d} da qui{where}.',
  'point.passes': 'Il percorso passa a {d} da qui{where}.',
  'point.onWay': ', su {name}',
  'point.moveMarker': 'Sposta il segnaposto',
  'point.moveMarkerTitle': 'Sposta il segnaposto dove il percorso incontra davvero la rete',
  'point.moveMarkerAria': 'Sposta il segnaposto dove il percorso incontra la rete',
  'point.keep': 'Lascia',
  'point.keepTitle': "Lascia il segnaposto dove l'hai messo",
  'point.clearField': 'Cancella {label}',
  'point.moveEarlier': 'Sposta questa tappa prima',
  'point.moveLater': 'Sposta questa tappa dopo',
  'point.remove': 'Rimuovi questa tappa',
  'point.searching': 'Ricerca…',
  'point.findingYou': 'Ti sto localizzando…',
  'point.useMyLocation': 'Usa la mia posizione',
  'point.myLocation': 'La mia posizione',
  'point.favourite': 'Preferito',
  'point.recent': 'Recente',
  'point.noGeolocation': 'Questo browser non può condividere una posizione.',
  'point.locationUnavailable': 'Posizione non disponibile.',

  // --- la risposta -----------------------------------------------------------
  'results.title': 'Risultati',
  'results.computing': 'Calcolo',
  'results.working': 'Sto cercando la via…',
  'results.tryAgain': 'Riprova',
  'results.noRouteGrade': 'Nessun percorso fino alla difficoltà {grade}.',
  'results.alpineOnly':
    'Solo su terreno alpinistico: ghiacciaio, corda e ramponi, non un sentiero segnalato.',
  'results.harderGrade': 'Quella via è aperta a una difficoltà superiore.',
  'results.moveNearer':
    'Spostalo più vicino a una strada o a un sentiero — puoi trascinare il segnaposto sulla mappa.',
  'results.tryTrail':
    "Prova un altro mezzo, oppure sposta l'arrivo più vicino a un sentiero segnalato.",
  'results.tryRoad': 'Prova un altro mezzo, oppure sposta un punto più vicino a una strada.',
  'results.allow': 'Consenti {grade}',
  'results.allowAria': 'Consenti {grade} e ricalcola',
  'results.recomputing': 'Ricalcolo',
  'results.degraded':
    'Al momento il servizio lavora su dati ridotti; questa risposta potrebbe essere meno precisa.',

  'empty.intro':
    'Scegli due punti e scopri quanto ci vuole — in auto, in bici o a piedi, con il dislivello e la difficoltà del sentiero lungo la via.',
  'empty.tryExample': 'Prova un esempio',
  'empty.example': 'Trento → Monte Stivo · auto + a piedi',

  // --- la scheda di un percorso ----------------------------------------------
  'card.routeAria': 'Percorso {n}: {duration}, {distance}',
  'card.fastest': 'Il più veloce',
  'card.hardest': 'Tratto più impegnativo del percorso: difficoltà {grade}',
  'card.onFoot': 'a piedi',
  'card.byCar': '{duration} in auto ({distance}, {ascent})',
  'card.byBike': '{duration} in bici ({distance}, {ascent})',
  'card.byLift': '{duration} in impianto ({ascent})',
  'card.walking': '{duration} a piedi ({distance})',
  'card.journeyJoin': ', poi ',
  'card.parkedAt': 'Parcheggio a {name}',
  'card.trailhead': "l'imbocco del sentiero",
  'card.legs': 'Tratte',
  'card.steps': 'Indicazioni',
  'card.linkLabel': 'Link a questo percorso',
  'card.copy': 'Copia',
  'card.done': 'Fatto',
  'card.share': 'Condividi',
  'card.saveDestination': 'Salva la destinazione',
  'card.linkCopied': 'Link copiato',
  'card.selectAndCopy': 'Seleziona il link e copialo.',
  'card.saved': '{name} salvato nei preferiti',
  'card.couldNotSave': 'Non è stato possibile salvare.',
  'card.destination': 'Destinazione',
  'card.savedPlace': 'Luogo salvato',

  'steps.showMore': 'Mostra altre {n}',
  'steps.showLess': 'Mostra meno',
  'leg.lift': 'Impianto',

  'profile.aria': 'Profilo altimetrico: da {from} a {to} su {distance}',
  'profile.at': 'a',
  'profile.hoverHint': 'Passa sul profilo per quota e distanza',
  'profile.touchHint': 'Tocca il profilo per quota e distanza',

  // --- la mappa --------------------------------------------------------------
  'map.aria': 'Mappa del Trentino-Alto Adige',
  'map.noWebgl': 'La mappa non può essere disegnata in questo browser.',
  'map.noWebglBody':
    "Non è riuscito ad avviare WebGL, che alla mappa serve. In Chrome apri {url}: se WebGL risulta non disponibile, attiva “Utilizza l'accelerazione grafica quando disponibile” in Impostazioni → Sistema, poi chiudi e riapri il browser. I percorsi funzionano anche senza mappa.",
  'map.start': 'Partenza',
  'map.destination': 'Arrivo',
  'map.via': 'Punto di passaggio',
  'map.stop': 'Tappa {n}',
  'map.markerAria': '{what}: {name}. Trascina per spostare.',
  'map.droppedPoint': 'punto sulla mappa',
  'map.crag': 'Falesia',

  'popover.actions': 'Azioni sul punto',
  'popover.locating': 'Individuazione…',
  'popover.avoided': 'Evitata',
  'popover.onYourRoute': 'Sul tuo percorso',
  'popover.close': 'Chiudi',
  'popover.removeVia': 'Rimuovi questo passaggio',
  'popover.rideLift': 'Prendi questo impianto',
  'popover.routeVia': 'Passa di qui',
  'popover.stopAvoiding': 'Non evitare più',
  'popover.avoidThis': 'Evita questa',
  'popover.startHere': 'Parti da qui',
  'popover.addStop': 'Aggiungi tappa',
  'popover.goHere': 'Vai qui',
  'popover.saveFavourite': 'Salva nei preferiti',
  'popover.name': 'Nome',

  'hint.click': 'Fai clic sulla mappa per impostare un punto.',
  'hint.tap': 'Tocca la mappa per impostare un punto.',
  'hint.gotIt': 'Ho capito',

  // --- il telefono -----------------------------------------------------------
  'sheet.openAria': 'Tocca o trascina per aprire i dettagli',
  'sheet.resizeAria': 'Trascina per ridimensionare il pannello, o tocca per aprirlo di più',
  'sheet.openTitle': 'Tocca o trascina per aprire',
  'sheet.resizeTitle': 'Trascina, o tocca per aprire di più',
  'sheet.hide': 'Nascondi i dettagli',
  'mobile.wholeRoute': 'Mostra tutto il percorso',
  'top.addParkStop': 'Aggiungi una tappa dove parcheggi',
  'top.search': 'Cerca',
  'top.menu': 'Menu',
  'top.travelMode': 'Mezzo di trasporto',
  'top.gradeGroup': 'Difficoltà per la parte a piedi',
  'top.lifts': 'Impianti',
  'menu.backToMap': 'Torna alla mappa',
  'menu.back': 'Indietro',
  'menu.addStop': 'Aggiungi una tappa',
  'menu.startOver': 'Ricomincia',
  'notes.avoiding': 'Eviti {name}',
  'notes.clearAll': 'Rimuovi tutto',
  'screen.choose': 'Scegli {label}',
  'screen.chooseOnMap': 'Scegli sulla mappa',
  'screen.favAndRecent': 'Preferiti e recenti',
  'screen.nothingFound': 'Nessun risultato con questo nome in Trentino-Alto Adige.',

  // --- cronologia e preferiti ------------------------------------------------
  'history.empty':
    'I percorsi che calcoli compaiono qui, così domani riprendi lo stesso piano.',
  'history.noRoute': 'Nessun percorso',
  'history.liftsAllowed': 'Impianti consentiti',
  'history.gradeTitle': 'Difficoltà {grade}',
  'history.deleteAria': 'Elimina {from} → {to}',
  'history.deleteAll': 'Elimina tutto',
  'history.unavailable': 'I percorsi recenti non sono disponibili al momento.',

  'fav.empty':
    'Metti una stella su un luogo della mappa, o salva la destinazione di un percorso, e ti aspetta qui per la prossima partenza.',
  'fav.name': 'Nome del preferito',
  'fav.save': 'Salva',
  'fav.cancel': 'Annulla',
  'fav.savedFor': 'Salvato per {mode}',
  'fav.rename': 'Rinomina {name}',
  'fav.delete': 'Elimina {name}',
  'fav.useAsStart': 'Usa come partenza',
  'fav.useAsDestination': 'Usa come arrivo',
  'fav.unavailable': 'I preferiti non sono disponibili al momento.',
  'fav.degraded':
    'I luoghi salvati non sono disponibili al momento. Il calcolo dei percorsi funziona comunque.',

  // --- impostazioni ----------------------------------------------------------
  'settings.theme': 'Tema',
  'settings.system': 'Sistema',
  'settings.light': 'Chiaro',
  'settings.dark': 'Scuro',
  'settings.language': 'Lingua',
  'settings.map': 'Mappa',
  'settings.terrain': 'Rilievo 3D',
  'settings.terrainHint':
    'Inclina la vista e solleva il rilievo, quando sei abbastanza vicino da vederlo.',
  'settings.contours': 'Curve di livello',
  'settings.contoursHint': 'Etichettate ogni 100 m, dallo zoom 10.',
  'settings.sat': 'Sentieri segnalati',
  'settings.satHint': 'Sentieri SAT, colorati per difficoltà.',
  'settings.pois': 'Cime, rifugi e passi',
  'settings.poisHint': 'Con nomi e quote.',
  'settings.crags': 'Falesie',
  'settings.cragsHint':
    'Gradi ed esposizione dove OpenStreetMap li riporta. Fitte attorno ad Arco, rade altrove.',
  'settings.lifts': 'Impianti di risalita',
  'settings.liftsHint': 'Funivie, cabinovie e seggiovie.',
  'settings.notPublished': 'Non ancora pubblicato.',
  'settings.units': 'Unità',
  'settings.unitsNote': 'Sistema metrico: chilometri, metri, ore e minuti.',
  'settings.about': 'Informazioni',
  'settings.aboutNote':
    'Mappa, sentieri e impianti da OpenStreetMap. Sentieri segnalati e toponimi da SAT e Provincia di Trento. Quote dalle tile pubbliche Terrarium. I tempi di percorrenza seguono la regola dei club alpini: distanza e dislivello contati insieme, dimezzando il minore dei due.',

  // --- informazioni ----------------------------------------------------------
  'about.intro':
    'Ometto pianifica una via attraverso il Trentino-Alto Adige a piedi, in bici o in auto, e ti dice quanto costa in tempo e in dislivello. È costruito su dati aperti e gira su una sola piccola macchina.',
  'about.planningAid': 'Uno strumento di pianificazione, non una guida',
  'about.data': 'Dati',
  'about.sources': 'Fonti',
  'about.limits': 'Limiti noti',
  'about.limit.dem.before': 'La quota è un modello di ',
  'about.limit.dem.em': 'superficie',
  'about.limit.dem.after':
    ": poggia sulle chiome degli alberi e sui tetti, così il dislivello nei boschi risulta un po' alto e una galleria non risulta affatto.",
  'about.limit.realtime':
    "Qui non c'è nulla in tempo reale. Chiusure, neve, caduta sassi, lavori e orari degli impianti non gli sono noti.",
  'about.limit.seasons':
    'Le stagioni non sono modellate. Un sentiero estivo e uno invernale sono la stessa linea su questa mappa.',
  'about.limit.crags':
    'Le falesie vengono solo da OpenStreetMap: fitte attorno ad Arco, rade altrove, e la fascia di gradi è quella che ha scritto un mappatore. Questo ti porta alla parete; non è una guida di arrampicata.',
  'about.idReplaced':
    "L'id anonimo di questo browser è stato sostituito con uno più lungo e privato. I preferiti e i percorsi recenti salvati con il vecchio id non sono più mostrati.",
  'about.date.osm': 'Estratto OpenStreetMap',
  'about.date.sat': 'Catasto SAT',
  'about.date.build': 'Build',
  'about.date.dem': 'Modello di elevazione',
  'about.date.lifts': 'Impianti',
  'about.src.osm': 'Contributori di OpenStreetMap',
  'about.src.osmNote':
    'Strade, sentieri, impianti, rifugi, falesie e toponimi. Open Database Licence.',
  'about.src.sat': 'SAT e Provincia di Trento',
  'about.src.satNote': 'Il catasto dei sentieri segnalati: numeri e difficoltà.',
  'about.src.omt': 'OpenMapTiles e OpenFreeMap',
  'about.src.omtNote': 'Lo schema delle vector tile e gli stili da cui questi derivano.',
  'about.src.terrain': 'AWS Terrain Tiles — Mapzen Terrarium',
  'about.src.terrainNote':
    "Le quote da cui sono disegnati l'ombreggiatura, le curve di livello e la vista 3D.",
  'about.src.copernicus': 'Copernicus GLO-30 DEM',
  'about.src.copernicusNote':
    'Il modello di elevazione dietro le terrain tile su questa regione.',
  'about.src.noto': 'Noto Sans',
  'about.src.notoNote': 'I caratteri sulla mappa. SIL Open Font Licence.',

  // --- avvertenze ------------------------------------------------------------
  'disclaimer.title': 'Uno strumento di pianificazione, non una guida',
  'disclaimer.accept': 'Ho capito',
  'disclaimer.times':
    'I tempi sono stime. Vengono da una regola empirica, non dalle tue gambe, dal tuo zaino o dal meteo.',
  'disclaimer.grades':
    'Le difficoltà dei sentieri vengono dal catasto SAT e da OpenStreetMap. Possono essere sbagliate, o semplicemente non aggiornate.',
  'disclaimer.alpine':
    'Il terreno alpinistico e le vie ferrate richiedono esperienza e attrezzatura. Questa app ci passa sopra, se glielo chiedi.',
  'disclaimer.seasons':
    'Impianti e strade di montagna hanno stagioni e orari. Una linea su questa mappa non è la promessa che sia aperta.',
  'disclaimer.responsibility':
    'Controlla le condizioni e le previsioni prima di partire. Della tua sicurezza sei responsabile tu.',

  // --- vocabolario -----------------------------------------------------------
  'mode.car': 'Auto',
  'mode.bike': 'Bici',
  'mode.hike': 'A piedi',
  'mode.car+hike': 'Auto + a piedi',
  'mode.bike+hike': 'Bici + a piedi',

  'legMode.car': 'In auto',
  'legMode.bike': 'In bici',
  'legMode.hike': 'A piedi',
  'legMode.lift': 'Impianto',

  'byMode.car': 'in auto',
  'byMode.bike': 'in bici',
  'byMode.hike': 'a piedi',
  'byMode.car+hike': 'in auto e a piedi',
  'byMode.bike+hike': 'in bici e a piedi',

  'liftType.cable_car': 'funivia',
  'liftType.gondola': 'cabinovia',
  'liftType.chair_lift': 'seggiovia',
  'liftType.mixed_lift': 'cabinovia e seggiovia',
  'liftType.other': 'impianto',

  'grade.T.name': 'Turistico',
  'grade.T.desc': 'sentieri larghi e ben segnalati, senza esposizione.',
  'grade.E.name': 'Escursionistico',
  'grade.E.desc': 'sentieri di montagna, passo sicuro, qualche tratto ripido.',
  'grade.EE.name': 'Escursionisti esperti',
  'grade.EE.desc': 'terreno ripido, esposto o roccioso.',
  'grade.EEA.name': 'Con attrezzatura',
  'grade.EEA.desc': 'via ferrata: imbrago, casco e set da ferrata.',
  'grade.A.name': 'Alpinistico',
  'grade.A.desc': 'ghiacciaio, corda e ramponi; non un sentiero segnalato.',

  'class.cycleway': 'ciclabile',
  'class.path': 'sentiero',
  'class.trail': 'sentiero segnalato',
  'class.track': 'strada forestale',
  'class.residential': 'strade urbane',
  'class.service': 'strade urbane',
  'class.tertiary': 'strada',
  'class.secondary': 'strada principale',
  'class.primary': 'strada principale',
  'class.motorway': 'autostrada',
  'class.trunk': 'superstrada',
  'class.pedestrian': 'area pedonale',
  'class.living_street': 'strade urbane',
  'class.unclassified': 'strada',
  'class.footway': 'sentiero',
  'class.steps': 'scalini',
  'class.ferry': 'traghetto',
  'class.minor': 'strada secondaria',
  'class.rail': 'ferrovia',

  'kind.place': 'Località',
  'kind.peak': 'Cima',
  'kind.hut': 'Rifugio',
  'kind.pass': 'Passo',
  'kind.crag': 'Falesia',
  'kind.street': 'Via',
  'kind.trail': 'Sentiero',
  'kind.saddle': 'Sella',

  'crag.routes': '{n} via | {n} vie',

  'fmt.lessThanMinute': '< 1 min',
  'fmt.hour': 'h',
  'fmt.minute': 'min',
  'fmt.today': 'Oggi',
  'fmt.yesterday': 'Ieri',

  // --- quello che può andare storto ------------------------------------------
  'error.unreachable': 'Il servizio di calcolo dei percorsi non è raggiungibile.',
  'error.tooMany': 'Troppe richieste, aspetta un momento.',
  'error.tooManyWait':
    'Troppe richieste, aspetta un momento — circa {n} secondo. | Troppe richieste, aspetta un momento — circa {n} secondi.',
  'error.timeout': 'Il calcolo del percorso ha impiegato troppo. Riprova.',
  'error.busy': 'Il servizio è occupato. Riprova tra un momento.',
  'error.notAvailable': 'Non ancora disponibile.',
  'error.server': 'Il servizio di calcolo dei percorsi ha avuto un problema. Riprova.',
  'error.generic': 'Non è stato possibile completare la richiesta.',
  'error.somethingWrong': 'Qualcosa è andato storto. Riprova.',

  // --- quello che il servizio dice di un percorso ----------------------------
  'svc.ferrataExcluded': 'vie ferrate escluse',
  'svc.usesFerrata': 'il percorso usa una via ferrata',
  'svc.alpine':
    'terreno alpinistico oltre EEA: ghiacciaio, corda e ramponi, non un sentiero segnalato',
  'svc.usesGradePath': 'il percorso usa un sentiero {grade}',
  'svc.liftsSeason': 'gli impianti funzionano solo in stagione; controlla le date del gestore',
  'svc.noElevation': 'nessun dato di quota',
  'svc.gradeExcluded': 'i sentieri oltre la difficoltà {grade} sono stati esclusi',
  'svc.parkedAt': 'parcheggio a {name}',
  'svc.parkedTrailhead': "parcheggio all'imbocco del sentiero",
  'svc.parkedStop': 'parcheggio alla tappa',
  'svc.twoPoints': 'un percorso ha bisogno di almeno due punti',
  'svc.noRoadNear': 'nessuna strada entro 1 km da {coord}',
  'svc.noRouteWithout': 'nessun percorso senza {name}',
  'svc.noRouteGrade': 'nessun percorso fino alla difficoltà {grade}',
  'svc.noRouteAt': 'nessun percorso a {grade}: il sentiero per {name} è {needed}',
  'svc.noModeRoute': 'nessun percorso {mode} tra questi punti',
  'svc.theDestination': "l'arrivo",

  // Come il motore chiama una strada che non ha un nome suo.
  'way.road': 'strada',
  'way.track': 'strada forestale',
  'way.path': 'sentiero',
  'way.lane': 'stradina',
  'way.street': 'strada',
  'way.unnamed': 'strada senza nome',
  'way.trail': 'sentiero {ref}',
  'way.cycleRoute': 'ciclabile {ref}',

  // --- posizione dal vivo ----------------------------------------------------
  'live.showPosition': 'Mostra la mia posizione',
  'live.followPosition': 'Segui la mia posizione',
  'live.stopFollowing': 'Smetti di seguire',
  'live.stop': 'Ferma',
  'live.stopAria': 'Smetti di usare la mia posizione',
  'live.dotAria': 'La tua posizione',
  'live.toGo': 'Mancano {distance} · {time}',
  'live.arriveAbout': 'arrivo previsto {time}',
  'live.stillToClimb': '{ascent} ancora da salire',
  'live.offRoute': 'Fuori dal percorso · a {distance}',
  'live.arrived': 'Sei a destinazione.',
  'live.waiting': 'In attesa di una posizione…',
  'live.accuracy': '±{m} m',
  'live.screenStaysOn': 'Lo schermo resta acceso mentre Ometto ti segue.',
  'live.denied':
    'La posizione è bloccata per questo sito. Consentila nelle impostazioni del sito nel browser.',
  'live.unavailable': 'Impossibile trovare la tua posizione.',
  'live.insecure': 'La posizione richiede una pagina sicura (https).',

  // --- offline ---------------------------------------------------------------
  'panel.title.saved': 'Percorsi salvati',

  'offline.pill': 'Offline · i percorsi salvati si aprono comunque',
  'offline.ready': "Pronto per l'uso offline",
  'offline.newVersion': "C'è una nuova versione",
  'offline.reload': 'Ricarica',
  'offline.savedCopy': 'Copia salvata · {date}',

  'offline.save': 'Salva offline',
  'offline.saving': 'Salvo la mappa…',
  'offline.savingPercent': 'Salvo la mappa… {n}%',
  'offline.saved': 'Salvato offline',
  'offline.openSaved': 'Apri i percorsi salvati',
  'offline.downloadMap': 'Scarica la mappa',
  'offline.aboutToDownload': 'circa {size}',
  'offline.mapSaved': 'Mappa salvata · {size}',
  'offline.mapIncomplete': 'La mappa non è scesa tutta. Riprova a scaricarla.',
  'offline.cancelled': 'Download interrotto. Il percorso resta salvato.',
  'offline.couldNotSave': 'Non è stato possibile salvarlo offline.',

  'offline.empty':
    'Salva un percorso dalla sua scheda: si aprirà qui anche senza connessione, con la sua mappa.',
  'offline.map': 'mappa {size}',
  'offline.noMap': 'mappa non scaricata',
  'offline.total': '{n} percorso · {size} | {n} percorsi · {size}',
  'offline.onDevice': '{size} su questo dispositivo',
  'offline.delete': 'Elimina',
  'offline.deleteAria': 'Elimina il percorso salvato da {from} a {to}',
  'offline.iosHint':
    'Su iPhone, aggiungi Ometto alla schermata Home per conservare i percorsi salvati senza limiti.',
  'offline.searchNeedsNetwork': 'Sei offline: la ricerca ha bisogno di una connessione.',
  // --- la scheda della domanda sul telefono
  'top.newRoute': 'Nuovo percorso',
  'top.editRoute': 'Modifica il percorso',
  'top.fold': 'Riduci la ricerca',
  'top.tripOptions': 'Difficoltà e impianti',
  'top.optionsAria': '{mode}, difficoltà {grade}. Difficoltà e impianti',
  'map.pointMoved': 'Punto spostato',
  'map.undo': 'Annulla',
  'map.holdToMove': 'Tieni premuto un punto per spostarlo',
  'top.liftsOn': 'impianti consentiti',
  'top.liftsOff': 'senza impianti',
  'top.prompt': 'Dove vuoi andare?',
  'top.unfold': 'Apri la ricerca',
};

export default it;
