(() => {
	var username = document.getElementById("username");
	var password = document.getElementById("password");
	var toggleBtn = document.getElementById("toggle-password");
	var eyeOpen = document.getElementById("eye-open");
	var eyeClosed = document.getElementById("eye-closed");
	var formMsg = document.getElementById("form-msg");

	function clearFieldInvalid() {
		username.setAttribute("aria-invalid", "false");
		password.setAttribute("aria-invalid", "false");
	}

	function setFieldInvalid() {
		username.setAttribute("aria-invalid", "true");
		password.setAttribute("aria-invalid", "true");
	}

	function replayShake(el) {
		if (!el || el.textContent.trim() === "") return;
		el.style.animation = "none";
		void el.offsetWidth;
		el.style.animation = "";
	}

	// Show/hide the password field.
	toggleBtn.addEventListener("click", () => {
		var isPassword = password.type === "password";
		password.type = isPassword ? "text" : "password";
		toggleBtn.setAttribute(
			"aria-label",
			isPassword ? "Hide password" : "Show password",
		);
		eyeOpen.style.display = isPassword ? "none" : "inline";
		eyeClosed.style.display = isPassword ? "inline" : "none";
	});

	// Clear the server error and invalid state as soon as the user starts correcting input.
	username.addEventListener("input", () => {
		clearFieldInvalid();
		formMsg.textContent = "";
	});
	password.addEventListener("input", () => {
		clearFieldInvalid();
		formMsg.textContent = "";
	});

	// When htmx swaps an error into #form-msg, mark fields invalid and replay the shake.
	formMsg.addEventListener("htmx:after:swap", () => {
		setFieldInvalid();
		replayShake(formMsg);
	});

	// Also apply field-invalid state and shake for server-rendered errors on initial load.
	if (formMsg && formMsg.textContent.trim() !== "") {
		setFieldInvalid();
		replayShake(formMsg);
	}
})();
