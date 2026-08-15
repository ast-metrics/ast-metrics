// Marks JS availability: entrance animations are gated behind this class so
// the landing page stays fully visible when JavaScript is unavailable.
document.documentElement.classList.add("js");

document.addEventListener("DOMContentLoaded", function () {
  // Intersection Observer for fade-up animations
  const observer = new IntersectionObserver((entries, observer) => {
    entries.forEach(entry => {
      if (entry.isIntersecting) {
        entry.target.classList.add("visible");
        observer.unobserve(entry.target);
      }
    });
  }, { root: null, rootMargin: "0px", threshold: 0.1 });

  document.querySelectorAll(".fade-up").forEach(el => observer.observe(el));

  // Install card: a vertical word roller cycles through package managers,
  // the terminal line follows with the matching command.
  const card = document.querySelector(".lp-install-card");

  if (card) {
    const roller = card.querySelector(".lp-roller");
    const track = card.querySelector(".lp-roller-track");
    const words = Array.from(track.children);
    const command = card.querySelector(".install-command");
    const copyButton = card.querySelector(".install-copy");
    let current = 0;
    let timer = null;
    let paused = false;

    // The roller is only as wide as the current word, so the sentence
    // closes up around short words like "go".
    const setWidth = () => {
      roller.style.width = words[current].offsetWidth + "px";
    };
    setWidth();
    if (document.fonts && document.fonts.ready) {
      document.fonts.ready.then(setWidth);
    }
    window.addEventListener("resize", setWidth);

    const select = (index) => {
      current = index;
      track.style.transform = "translateY(-" + index * 1.3 + "em)";
      setWidth();
      command.classList.add("switching");
      setTimeout(() => {
        command.textContent = words[index].dataset.command;
        command.classList.remove("switching");
      }, 180);
    };

    const stop = () => {
      if (timer) {
        clearInterval(timer);
        timer = null;
      }
    };

    // Auto-rotation is an invitation, not a carousel ride: it stops for good
    // as soon as the visitor copies a command.
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (!reducedMotion) {
      timer = setInterval(() => {
        if (!paused) {
          select((current + 1) % words.length);
        }
      }, 2600);
    }

    card.addEventListener("mouseenter", () => { paused = true; });
    card.addEventListener("mouseleave", () => { paused = false; });
    card.addEventListener("focusin", () => { paused = true; });
    card.addEventListener("focusout", () => { paused = false; });

    roller.addEventListener("click", () => {
      select((current + 1) % words.length);
    });

    copyButton.addEventListener("click", () => {
      stop();
      navigator.clipboard.writeText(words[current].dataset.command).then(() => {
        copyButton.classList.add("copied");
        setTimeout(() => copyButton.classList.remove("copied"), 2000);
      });
    });
  }
});
